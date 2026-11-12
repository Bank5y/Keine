package course

import (
	"net/url"
	"path"
	"strings"
)

// How the reader plays a lesson's video.
const (
	VideoPlayer = "player" // our own player: packaged files and direct media links
	VideoEmbed  = "embed"  // the platform's embedded player
	VideoLink   = "link"   // a page we cannot embed; opened in a new tab
)

var directMedia = map[string]bool{".mp4": true, ".m4v": true, ".webm": true, ".ogv": true, ".mov": true, ".m3u8": true}

// ClassifyVideoURL decides how an external video link is played and returns
// the URL to play or embed.
func ClassifyVideoURL(raw string) (mode, src string) {
	u, err := url.Parse(raw)
	if err != nil {
		return VideoLink, raw
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	host = strings.TrimPrefix(host, "m.")
	switch {
	case directMedia[strings.ToLower(path.Ext(u.Path))]:
		return VideoPlayer, raw
	case host == "bilibili.com" && strings.HasPrefix(u.Path, "/video/"):
		if id := strings.Trim(strings.TrimPrefix(u.Path, "/video/"), "/"); id != "" {
			q := url.Values{"autoplay": {"0"}}
			if strings.HasPrefix(strings.ToUpper(id), "AV") {
				q.Set("aid", id[2:])
			} else {
				q.Set("bvid", id)
			}
			if p := u.Query().Get("p"); p != "" {
				q.Set("p", p)
			}
			return VideoEmbed, "https://player.bilibili.com/player.html?" + q.Encode()
		}
	case host == "youtube.com" || host == "youtube-nocookie.com":
		id := u.Query().Get("v")
		if rest, ok := strings.CutPrefix(u.Path, "/embed/"); ok {
			id = rest
		}
		if rest, ok := strings.CutPrefix(u.Path, "/shorts/"); ok {
			id = rest
		}
		if id != "" {
			return VideoEmbed, "https://www.youtube-nocookie.com/embed/" + url.PathEscape(strings.Trim(id, "/"))
		}
	case host == "youtu.be":
		if id := strings.Trim(u.Path, "/"); id != "" {
			return VideoEmbed, "https://www.youtube-nocookie.com/embed/" + url.PathEscape(id)
		}
	}
	return VideoLink, raw
}
