package db

type ManifestItemType string

const (
	FileItem      ManifestItemType = `file`
	DirectoryItem                  = `directory`
)

type ManifestField interface{}

type ManifestItem struct {
	ID           string
	Type         ManifestItemType
	RelativePath string
	Fields       []ManifestField
}

type Manifest []ManifestItem
