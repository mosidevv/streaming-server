package endpoint

import (
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/juanvallejo/streaming-server/pkg/api/config"
)

const YOUTUBE_ENDPOINT_PREFIX = "/youtube"

var (
	youtubeMaxResults       = 20
	youtubeEndpointTemplate = "https://www.googleapis.com/youtube/v3/search?part=snippet&q=%v&type=video&maxResults=%v&key=%v"
)

// YoutubeEndpoint implements ApiEndpoint
type YoutubeEndpoint struct {
	*ApiEndpointSchema
}

// Handle returns a "discovery" of all local streams in the server data root.
func (e *YoutubeEndpoint) Handle(segments []string, w http.ResponseWriter, r *http.Request) {
	switch {
	case segments[1] == "search":
		if len(segments) < 3 {
			HandleEndpointError(fmt.Errorf("not enough arguments: /search/query"), w)
			return
		}

		handleApiSearch(segments[2], w)
		return
	}

	HandleEndpointError(fmt.Errorf("unimplemented parameter"), w)
}

func handleApiSearch(searchQuery string, w http.ResponseWriter) {
	reqUrl := fmt.Sprintf(youtubeEndpointTemplate, searchQuery, youtubeMaxResults, config.YT_API_KEY)
	res, err := http.Get(reqUrl)
	if err != nil {
		HandleEndpointError(err, w)
		return
	}

	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		HandleEndpointError(err, w)
		return
	}

	w.Write(data)
}

func NewYoutubeEndpoint() ApiEndpoint {
	return &YoutubeEndpoint{
		&ApiEndpointSchema{
			path: YOUTUBE_ENDPOINT_PREFIX,
		},
	}
}
