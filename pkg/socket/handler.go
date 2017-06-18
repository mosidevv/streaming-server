package socket

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	sockio "github.com/googollee/go-socket.io"

	"github.com/juanvallejo/streaming-server/pkg/playback"
	"github.com/juanvallejo/streaming-server/pkg/socket/client"
	"github.com/juanvallejo/streaming-server/pkg/socket/cmd"
	"github.com/juanvallejo/streaming-server/pkg/socket/util"
	"github.com/juanvallejo/streaming-server/pkg/stream"
)

type Handler struct {
	clientHandler   client.SocketClientHandler
	CommandHandler  cmd.SocketCommandHandler
	PlaybackHandler playback.StreamPlaybackHandler
	StreamHandler   stream.StreamHandler

	server *Server
}

const (
	ROOM_DEFAULT_LOBBY           = "lobby"
	ROOM_DEFAULT_STREAMSYNC_RATE = 30 // send streamsync to clients every 30 seconds
)

func (h *Handler) HandleClientConnection(conn sockio.Socket) {
	log.Printf("INFO SOCKET CONN client (%s) has connected with id %q\n", conn.Request().RemoteAddr, conn.Id())

	h.RegisterClient(conn)
	log.Printf("INFO SOCKET currently %v clients registered\n", h.clientHandler.GetClientSize())

	// TODO: remove room's StreamPlayback once last client has left
	conn.On("disconnection", func() {
		log.Printf("INFO DCONN SOCKET client with id %q has disconnected\n", conn.Id())

		if c, err := h.clientHandler.GetClient(conn.Id()); err == nil {
			userName, exists := c.GetUsername()
			if exists {
				c.BroadcastFrom("info_clientleft", &client.Response{
					Id:   conn.Id(),
					From: userName,
				})
			}
		}

		err := h.DeregisterClient(conn)
		if err != nil {
			log.Printf("ERR SOCKET %v", err)
		}
	})

	conn.On("request_updateusername", func(data map[string]string) {
		username, ok := data["user"]
		if !ok {
			log.Printf("ERR SOCKET CLIENT client %q sent malformed request to update username. Ignoring request.", conn.Id())
			return
		}

		c, err := h.clientHandler.GetClient(conn.Id())
		if err != nil {
			log.Printf("ERR SOCKET CLIENT %v. Broadcasting as info_clienterror event", err)
			c.BroadcastErrorTo(err)
			return
		}

		err = util.UpdateClientUsername(c, username, h.clientHandler, h.PlaybackHandler)
		if err != nil {
			log.Printf("ERR SOCKET CLIENT %v. Broadcasting as \"info_clienterror\" event", err)
			c.BroadcastErrorTo(err)
		}
	})

	conn.On("request_chatmessage", func(data map[string]interface{}) {
		username, ok := data["user"]
		if ok {
			log.Printf("INFO SOCKET CLIENT client with id %q requested a chat message broadcast with name %q", conn.Id(), username)
		}

		c, err := h.clientHandler.GetClient(conn.Id())
		if err != nil {
			log.Printf("ERR SOCKET CLIENT could not retrieve client. Ignoring request_chatmessage request: %v", err)
			return
		}

		err, command, isCommand := h.ParseCommandMessage(c, data)
		if err != nil {
			log.Printf("ERR SOCKET CLIENT unable to parse client chat message as command: %v", err)
			c.BroadcastSystemMessageTo(err.Error())
			return
		}

		if isCommand {
			cmdSegments := strings.Split(command, " ")
			cmdArgs := []string{}
			if len(cmdSegments) > 1 {
				cmdArgs = cmdSegments[1:]
			}

			log.Printf("INFO SOCKET CLIENT interpreting chat message as user command %q for client id (%q) with name %q", command, conn.Id(), username)
			result, err := h.CommandHandler.ExecuteCommand(cmdSegments[0], cmdArgs, c, h.clientHandler, h.PlaybackHandler, h.StreamHandler)
			if err != nil {
				log.Printf("ERR SOCKET CLIENT unable to execute command with id %q: %v", command, err)
				c.BroadcastSystemMessageTo(err.Error())
				return
			}

			if len(result) > 0 {
				c.BroadcastSystemMessageTo(result)
			}
			return
		}

		// TODO: parse message multimedia
		// if err := h.ReplaceMessageImageURL(data); err != nil {
		// 	log.Printf("SOCKET CLIENT WARN ")
		// }

		res := client.ResponseFromClientData(data)
		c.BroadcastAll("chatmessage", &res)

		fmt.Printf("INFO SOCKET CLIENT chatmessage received %v\n", data)
	})

	conn.On("request_streamsync", func(data map[string]interface{}) {
		log.Printf("INFO SOCKET CLIENT client with id %q requested a streamsync", conn.Id())

		c, err := h.clientHandler.GetClient(conn.Id())
		if err != nil {
			log.Printf("ERR SOCKET CLIENT unable to retrieve client from connection id. Ignoring request_streamsync request: %v", err)
			return
		}

		roomName, exists := c.GetRoom()
		if !exists {
			log.Printf("ERR SOCKET CLIENT client with id (%q) has no room association. Ignoring streamsync request.", c.GetId())
			return
		}

		sPlayback, exists := h.PlaybackHandler.GetStreamPlayback(roomName)
		if !exists {

		}

		c.BroadcastTo("streamsync", &client.Response{
			Id:    c.GetId(),
			Extra: sPlayback.GetStatus(),
		})
	})
}

// ParseCommandMessage receives a client pointer and a data map sent by a client
// and determines whether the "message" field in the client data map contains a
// valid client command. An error is returned if there are any errors while parsing
// the message field. A boolean (true) is returned if the message field contains a
// valid client command, along with a string ("command") containing a StreamCommand id
//
// A valid client command will always begin with a "/" and never contain more than
// one "/" character.
func (h *Handler) ParseCommandMessage(client *client.Client, data map[string]interface{}) (error, string, bool) {
	message, ok := data["message"]
	if !ok {
		return fmt.Errorf("error: invalid client command format; message field empty"), "", false
	}

	command, ok := message.(string)
	if !ok {
		return fmt.Errorf("error: client command parse error; unable to cast message to string"), "", false
	}

	if string(command[0]) != "/" {
		return nil, "", false
	}

	return nil, command[1:], true
}

// RegisterClient receives a socket connection, creates a new client, and assigns the client to a room.
// if client is first to join room, then the room did not exist before; if this is the case, the following steps
// are then taken to "create" a room:
// 1. create a streamPlayback object whose "id" becomes the room's name
// 2.
func (h *Handler) RegisterClient(sockioconn sockio.Socket) {
	log.Printf("INFO SOCKET CLIENT registering client with id %q\n", sockioconn.Id())

	roomName, err := util.GetRoomNameFromRequest(sockioconn.Request())
	if err != nil {
		log.Printf("WARN SOCKET CLIENT websocket connection initiated outside of a valid room. Assigning default lobby room %q.", ROOM_DEFAULT_LOBBY)
		roomName = ROOM_DEFAULT_LOBBY
	}

	log.Printf("INFO SOCKET CLIENT assigning client to room with name %q", roomName)

	c := h.clientHandler.CreateClient(sockioconn)
	c.JoinRoom(roomName)

	c.BroadcastFrom("info_clientjoined", &client.Response{
		Id: c.GetId(),
	})

	sPlayback, exists := h.PlaybackHandler.GetStreamPlayback(roomName)
	if !exists {
		log.Printf("INFO SOCKET CLIENT StreamPlayback did not exist for room with name %q. Creating...", roomName)
		sPlayback = h.PlaybackHandler.NewStreamPlayback(roomName)
		sPlayback.OnTick(func(currentTime int) {
			if currentTime%ROOM_DEFAULT_STREAMSYNC_RATE != 0 {
				return
			}

			currPlayback, exists := h.PlaybackHandler.GetStreamPlayback(roomName)
			if !exists {
				log.Printf("ERR CALLBACK-PLAYBACK SOCKET CLIENT attempted to send streamsync event to client, but stream playback does not exist.")
			}

			log.Printf("INFO CALLBACK-PLAYBACK SOCKET CLIENT streamsync event sent after %v seconds", currentTime)
			c.BroadcastAll("streamsync", &client.Response{
				Id:    sockioconn.Id(),
				Extra: currPlayback.GetStatus(),
			})
		})

		return
	}

	log.Printf("INFO SOCKET CLIENT found StreamPlayback for room with name %q", roomName)

	pStream, exists := sPlayback.GetStream()
	if exists {
		log.Printf("INFO SOCKET CLIENT found stream info (%s) associated with StreamPlayback for room with name %q... Sending \"streamload\" signal to client", (*pStream).GetStreamURL(), roomName)
		c.BroadcastTo("streamload", &client.Response{
			Id:    c.GetId(),
			Extra: (*pStream).GetInfo(),
		})
	}
}

func (h *Handler) DeregisterClient(sockioconn sockio.Socket) error {
	err := h.clientHandler.DestroyClient(sockioconn)
	if err != nil {
		return fmt.Errorf("error: unable to de-register client: %v", err)
	}
	return nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.server.ServeHTTP(w, r)
}

func NewHandler(commandHandler cmd.SocketCommandHandler, clientHandler client.SocketClientHandler, playbackHandler playback.StreamPlaybackHandler, streamHandler stream.StreamHandler) *Handler {
	socketServer, err := NewServer(nil)
	if err != nil {
		log.Fatal(err)
	}

	handler := &Handler{
		clientHandler:   clientHandler,
		CommandHandler:  commandHandler,
		PlaybackHandler: playbackHandler,
		StreamHandler:   streamHandler,

		server: socketServer,
	}

	handler.addRequestHandlers()
	return handler
}

func (h *Handler) addRequestHandlers() {
	h.server.On("connection", func(sockioconn sockio.Socket) {
		h.HandleClientConnection(sockioconn)
	})
}
