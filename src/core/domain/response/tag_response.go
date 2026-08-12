package response

import "time"

type TagCategoryView struct {
	ID    string `json:"id"`
	Code  string `json:"code"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type TagView struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Category   TagCategoryView `json:"category"`
	Sources    []string        `json:"sources,omitempty"`
	Visibility string          `json:"visibility"`
	Automatic  bool            `json:"automatic"`
	Suppressed bool            `json:"suppressed,omitempty"`
}

type CompactTagView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	CategoryCode string `json:"category_code"`
	Color        string `json:"color"`
	Visibility   string `json:"visibility"`
	SystemCode   string `json:"system_code,omitempty"`
}

type FileTagsResponse struct {
	FileID     string    `json:"file_id"`
	Tags       []TagView `json:"tags"`
	Suppressed []TagView `json:"suppressed"`
	State      string    `json:"state"`
	LastError  string    `json:"last_error,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

type DirectoryTagsResponse struct {
	DirectoryID int       `json:"directory_id"`
	Tags        []TagView `json:"tags"`
}

type TagPreviewItem struct {
	Input string           `json:"input"`
	Tags  []CompactTagView `json:"tags"`
}

type TagCloudItem struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	BaseName     string          `json:"base_name"`
	Category     TagCategoryView `json:"category"`
	BaseCategory TagCategoryView `json:"base_category"`
	FileCount    int64           `json:"file_count"`
	Hidden       bool            `json:"hidden"`
	System       bool            `json:"system"`
	SystemCode   string          `json:"system_code,omitempty"`
}

type TagCloudResponse struct {
	Tags   []TagCloudItem `json:"tags"`
	Hidden []TagCloudItem `json:"hidden"`
}

type TagCloudEditorResponse struct {
	Tag     TagCloudItem `json:"tag"`
	Aliases []string     `json:"aliases"`
}
