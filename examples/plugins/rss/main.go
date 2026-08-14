package main

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	myobjplugin "github.com/H2SYJ/MyObj/sdk/tinygo"
)

type handler struct{}

func (handler) Healthcheck() error { return nil }

func (handler) ValidateConfig(config map[string]interface{}) error {
	value, _ := config["feed_url"].(string)
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("订阅地址必须是有效的HTTP/HTTPS URL")
	}
	return nil
}

func (h handler) Fetch(request myobjplugin.InvocationRequest) ([]myobjplugin.DownloadableItem, error) {
	if err := h.ValidateConfig(request.Config); err != nil {
		return nil, err
	}
	feedURL := strings.TrimSpace(request.Config["feed_url"].(string))
	relativeSavePath, _ := request.Config["relative_save_path"].(string)
	response, err := myobjplugin.HTTPRequest(myobjplugin.HTTPRequestInput{Method: "GET", URL: feedURL})
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("订阅源返回%d", response.StatusCode)
	}
	body, err := response.Body()
	if err != nil {
		return nil, err
	}
	return parseFeed(body, relativeSavePath)
}

type rssDocument struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	GUID      string `xml:"guid"`
	Title     string `xml:"title"`
	Published string `xml:"pubDate"`
	Enclosure struct {
		URL string `xml:"url,attr"`
	} `xml:"enclosure"`
	Thumbnail struct {
		URL string `xml:"url,attr"`
	} `xml:"thumbnail"`
}

type atomDocument struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID        string     `xml:"id"`
	Title     string     `xml:"title"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
	Links     []atomLink `xml:"link"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

func parseFeed(content []byte, relativeSavePath string) ([]myobjplugin.DownloadableItem, error) {
	var rss rssDocument
	if err := xml.Unmarshal(content, &rss); err == nil && len(rss.Channel.Items) > 0 {
		items := make([]myobjplugin.DownloadableItem, 0, len(rss.Channel.Items))
		for _, item := range rss.Channel.Items {
			if item.Enclosure.URL == "" {
				continue
			}
			items = append(items, downloadable(item.GUID, item.Title, item.Enclosure.URL, item.Thumbnail.URL, item.Published, relativeSavePath))
		}
		return items, nil
	}
	var atom atomDocument
	if err := xml.Unmarshal(content, &atom); err != nil {
		return nil, fmt.Errorf("订阅源不是有效的RSS或Atom")
	}
	items := make([]myobjplugin.DownloadableItem, 0, len(atom.Entries))
	for _, entry := range atom.Entries {
		for _, link := range entry.Links {
			if link.Rel == "enclosure" && link.Href != "" {
				published := entry.Published
				if published == "" {
					published = entry.Updated
				}
				items = append(items, downloadable(entry.ID, entry.Title, link.Href, "", published, relativeSavePath))
				break
			}
		}
	}
	return items, nil
}

func downloadable(id, title, rawURL, thumbnailURL, published, relativeSavePath string) myobjplugin.DownloadableItem {
	downloadType := "http"
	if parsed, err := url.Parse(rawURL); err == nil && strings.EqualFold(path.Ext(parsed.Path), ".m3u8") {
		downloadType = "hls"
	}
	var publishedAt *time.Time
	for _, layout := range []string{time.RFC3339, time.RFC1123Z, time.RFC1123} {
		if value, err := time.Parse(layout, published); err == nil {
			publishedAt = &value
			break
		}
	}
	return myobjplugin.DownloadableItem{ID: id, Title: title, URL: rawURL, DownloadType: downloadType,
		RelativeSavePath: relativeSavePath, ThumbnailURL: thumbnailURL, PublishedAt: publishedAt}
}

func main() { myobjplugin.Run(handler{}) }
