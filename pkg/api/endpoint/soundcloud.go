package endpoint

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/juanvallejo/streaming-server/pkg/api/config"
	"github.com/juanvallejo/streaming-server/pkg/socket/connection"
)

const (
	SC_ENDPOINT_PREFIX = "/soundcloud"

	SoundCloudPlaylistItem = "soundcloud#playlistItem"
	SoundCloudStreamItem   = "soundcloud#stream"
)

var (
	soundCloudEndpointTemplate        = "http://api.soundcloud.com/tracks?q=%s&client_id=%v"
	soundCloudSearchEndpointTemplate  = "http://api.soundcloud.com/tracks?q=%s&client_id=%v"
	soundCloudResolveEndpointTemplate = "https://api.soundcloud.com/resolve.json?url=%s&client_id=%s"
)

type SoundCloudPlaylist struct {
	Tracks []*SoundCloudItem `json:"tracks"`
}

type SoundCloudItem struct {
	*EndpointResponseItem

	Id        int    `json:"id"`
	Permalink string `json:"permalink_url"`
	Artwork   string `json:"artwork_url"`

	Errors []SoundCloudEndpointError `json:"errors"`
}

type SoundCloudEndpointError struct {
	Message string `json:"error_message"`
}

type SoundCloudEndpointResponse struct {
	Items []*SoundCloudItem `json:"items"`
}

// SoundCloudEndpoint implements ApiEndpoint
type SoundCloudEndpoint struct {
	*ApiEndpointSchema
}

// Handle returns a "discovery" of all local streams in the server data root.
func (e *SoundCloudEndpoint) Handle(connHandler connection.ConnectionHandler, segments []string, w http.ResponseWriter, r *http.Request) {
	if len(segments) < 2 {
		HandleEndpointError(fmt.Errorf("unimplemented endpoint"), w)
		return
	}

	// since we are dealing with a url value, split
	// the un-sanitized variant of the request path
	// containing the url encoded value
	segments = strings.Split(r.URL.String(), "/")
	segments = segments[2:]

	switch {
	case segments[1] == "search":
		if len(segments) < 3 {
			HandleEndpointError(fmt.Errorf("not enough arguments: /seatch/term"), w)
			return
		}

		handleSoundCloudApiSearch(segments[2], w)
		return
	case segments[1] == "stream":
		if len(segments) < 3 {
			HandleEndpointError(fmt.Errorf("not enough arguments: /stream/url"), w)
			return
		}

		handleSoundCloudApiStream(strings.Join(segments[2:], "/"), w)
		return
	}

	HandleEndpointError(fmt.Errorf("unimplemented parameter"), w)
}

func handleSoundCloudApiSearch(query string, w http.ResponseWriter) {
	reqUrl := fmt.Sprintf(soundCloudSearchEndpointTemplate, query, config.SC_API_KEY)
	handleSoundCloudApiRequest(reqUrl, w)
}

func handleSoundCloudApiStream(rawPermalink string, w http.ResponseWriter) {
	permalink := url.QueryEscape(rawPermalink)

	// resolve permalink into track id
	resolveUrl := fmt.Sprintf(soundCloudResolveEndpointTemplate, permalink, config.SC_API_KEY)
	res, err := http.Get(resolveUrl)
	if err != nil {
		HandleEndpointError(err, w)
		return
	}

	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		HandleEndpointError(err, w)
		return
	}

	if len(data) == 0 {
		HandleEndpointError(fmt.Errorf("item not available for playback"), w)
		return
	}

	respBytes, err := encodeApiResponse(data)
	if err != nil {
		HandleEndpointError(err, w)
		return
	}

	w.Write(respBytes)

}

func encodeApiResponse(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("no data to encode")
	}

	resp := &SoundCloudEndpointResponse{}
	item := &SoundCloudItem{}
	err := json.Unmarshal(data, &item)
	if err != nil {
		return nil, err
	}

	if len(item.Errors) > 0 {
		return nil, fmt.Errorf("error: %v", item.Errors[0].Message)
	}

	if item.Kind == "playlist" {
		playlist := &SoundCloudPlaylist{}
		if err := json.Unmarshal(data, &playlist); err != nil {
			return nil, err
		}

		for _, track := range playlist.Tracks {
			track.Kind = SoundCloudPlaylistItem
			track.Thumb = track.Artwork
			track.Url = track.Permalink
			resp.Items = append(resp.Items, track)
		}

		respBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, err
		}

		return respBytes, nil
	}

	// default required spec fields for an api response item
	item.Thumb = item.Artwork
	item.Url = item.Permalink

	item.Kind = SoundCloudStreamItem
	resp.Items = append(resp.Items, item)

	respBytes, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}

	return respBytes, nil
}

func handleSoundCloudApiRequest(reqUrl string, w http.ResponseWriter) {
	res, err := http.Get(reqUrl)
	if err != nil {
		HandleEndpointError(err, w)
		return
	}

	defer res.Body.Close()

	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		HandleEndpointError(err, w)
		return
	}

	respBytes, err := encodeApiResponse(data)
	if err != nil {
		HandleEndpointError(err, w)
		return
	}

	w.Write(respBytes)
}

func NewSoundCloudEndpoint() ApiEndpoint {
	return &SoundCloudEndpoint{
		&ApiEndpointSchema{
			path: SC_ENDPOINT_PREFIX,
		},
	}
}
