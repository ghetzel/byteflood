package db

import (
	"fmt"
	"github.com/ghetzel/pivot/dal"
	"os"
	"regexp"
)

var MetadataSchema = dal.Collection{
	Name:              `metadata`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name:     `name`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `parent`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name: `size`,
			Type: dal.IntType,
		}, {
			Name: `checksum`,
			Type: dal.StringType,
		}, {
			Name:     `label`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `directory`,
			Type:     dal.BooleanType,
			Required: true,
		}, {
			Name: `children`,
			Type: dal.IntType,
		}, {
			Name: `descendants`,
			Type: dal.IntType,
		}, {
			Name:     `last_modified_at`,
			Type:     dal.IntType,
			Required: true,
		}, {
			Name: `metadata`,
			Type: dal.ObjectType,
		},
	},
}

var SharesSchema = dal.Collection{
	Name:              `shares`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name: `filter`,
			Type: dal.StringType,
		}, {
			Name: `description`,
			Type: dal.StringType,
		}, {
			Name: `filter_templates`,
			Type: dal.StringType,
		},
	},
}

var DownloadsSchema = dal.Collection{
	Name: `downloads`,
	Fields: []dal.Field{
		{
			Name:     `status`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name: `priority`,
			Type: dal.IntType,
		}, {
			Name:     `peer_name`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `session_id`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `share_id`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `file_id`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `name`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `destination`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `size`,
			Type:     dal.IntType,
			Required: true,
		}, {
			Name:     `added_at`,
			Type:     dal.TimeType,
			Required: true,
		}, {
			Name: `error`,
			Type: dal.StringType,
		},
	},
}

// keyed on public peer ID
var AuthorizedPeersSchema = dal.Collection{
	Name:              `authorized_peers`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name:     `name`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name: `group`,
			Type: dal.StringType,
		}, {
			Name: `addresses`,
			Type: dal.StringType,
		},
	},
}

var SystemSchema = dal.Collection{
	Name:              `system`,
	IdentityField:     `key`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name: `value`,
			Type: dal.ObjectType,
		},
	},
}

var ScannedDirectoriesSchema = dal.Collection{
	Name:              `scanned_directories`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name:        `path`,
			Description: `A local filesystem path that will be scanned for files.`,
			Type:        dal.StringType,
			Validator: func(value interface{}) error {
				if s, err := os.Stat(fmt.Sprintf("%v", value)); err == nil {
					if !s.IsDir() {
						return fmt.Errorf("Given path must be a directory")
					}
				} else {
					return err
				}

				return nil
			},
		}, {
			Name: `file_pattern`,
			Description: `An optional regular expression used to whitelist filenames. ` +
				`If set, only absolute paths matching this query will be scanned.`,
			Type: dal.StringType,
			Validator: func(value interface{}) error {
				if _, err := regexp.Compile(fmt.Sprintf("%v", value)); err != nil {
					return err
				}

				return nil
			},
		}, {
			Name:         `recursive`,
			Description:  `Whether to recursively scan subdirectories under this root directory.`,
			Type:         dal.BooleanType,
			Required:     true,
			DefaultValue: true,
		}, {
			Name:        `min_file_size`,
			Description: `If set, files smaller than this value will be skipped.`,
			Type:        dal.IntType,
		}, {
			Name: `checksum`,
			Description: `If true, all scanned files will have a SHA-256 checksum calculated for them. ` +
				`Note: this will significantly increase the time to scan large directories.`,
			Type:         dal.BooleanType,
			Required:     true,
			DefaultValue: false,
		},
	},
}

var SubscriptionsSchema = dal.Collection{
	Name: `subscriptions`,
	Fields: []dal.Field{
		{
			Name:        `source_group`,
			Description: `The peer name or @-prefixed peer group to source data from.`,
			Type:        dal.StringType,
			Required:    true,
		}, {
			Name:        `share_name`,
			Description: `Name of the remote share to query for data.`,
			Type:        dal.StringType,
			Required:    true,
		}, {
			Name:        `target_path`,
			Description: `The local filesystem path where downloaded data will be stored.`,
			Type:        dal.StringType,
			Required:    true,
		}, {
			Name:        `filter`,
			Description: `A filter used to narrow down which files to monitor from the named share.`,
			Type:        dal.StringType,
		}, {
			Name:        `bytes_downloaded`,
			Description: `How much data has been downloaded within the current quota interval.`,
			Type:        dal.IntType,
		}, {
			Name:        `quota`,
			Description: `The maximum number of bytes to download within a given span of time.`,
			Type:        dal.IntType,
		}, {
			Name:        `quota_reset_at`,
			Description: `The last time the quota was reset.`,
			Type:        dal.TimeType,
		}, {
			Name:        `quota_interval`,
			Description: `The amount of time (in seconds) that a quota applies to.`,
			Type:        dal.IntType,
		},
	},
}
