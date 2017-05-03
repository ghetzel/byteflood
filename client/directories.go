package client

import (
	"fmt"

	"github.com/ghetzel/go-stockutil/typeutil"
)

type Directory struct {
	ID              string `json:"id"`
	Path            string `json:"path"`
	FilePattern     string `json:"file_pattern"`
	Recursive       bool   `json:"recursive"`
	MinimumFileSize int64  `json:"min_file_size"`
	FollowSymlinks  bool   `json:"follow_symlinks"`
}

func (self *Client) GetDirectories() (output *[]Directory, err error) {
	err = self.Retrieve(`directories`, nil, &output)
	return
}

func (self *Client) GetDirectory(id string) (output *Directory, err error) {
	err = self.Retrieve(`directories`, id, &output)
	return
}

func (self *Client) GetDirectoryByPath(path string) (*Directory, error) {
	var all []*Directory

	if err := self.Retrieve(`directories`, nil, &all); err == nil {
		for _, directory := range all {
			if directory.Path == path {
				return directory, nil
			}
		}

		return nil, fmt.Errorf("No such directory with path %s", path)
	} else {
		return nil, err
	}
}

func (self *Client) CreateDirectory(id string, path string, options *Directory) error {
	if typeutil.IsEmpty(path) {
		return fmt.Errorf("%q cannot be empty", `path`)
	}

	if options == nil {
		options = &Directory{}
	}

	options.ID = id
	options.Path = path

	return self.Create(`directories`, options)
}

func (self *Client) RemoveDirectory(id string) error {
	return self.Delete(`directories`, id)
}

// func (self *Client) ScanDirectory(id string) error {
// }

// func (self *Client) CleanupDirectory(id string) error {
// }
