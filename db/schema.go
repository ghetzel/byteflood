package db

import (
	"github.com/ghetzel/pivot/dal"
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
			Name:     `label`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `directory`,
			Type:     dal.BooleanType,
			Required: true,
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
	Name: `shares`,
	Fields: []dal.Field{
		{
			Name:     `name`,
			Type:     dal.StringType,
			Required: true,
			Unique:   true,
		}, {
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
			Name:     `session_id`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `file_id`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `filename`,
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
			Name:     `peer_name`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name: `error`,
			Type: dal.StringType,
		}, {
			Name:     `added_at`,
			Type:     dal.TimeType,
			Required: true,
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
		},
	},
}

var SystemSchema = dal.Collection{
	Name:              `system`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name:     `key`,
			Type:     dal.StringType,
			Identity: true,
		}, {
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
			Identity:    true,
		}, {
			Name:        `label`,
			Description: `A short label what will be used to identify this group of files, typically for use with share filters.`,
			Type:        dal.StringType,
			Required:    true,
		}, {
			Name: `file_pattern`,
			Description: `An optional regular expression used to whitelist filenames. ` +
				`If set, only absolute paths matching this query will be scanned.`,
			Type: dal.StringType,
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
			Name:         `filter`,
			Description:  `A filter used to narrow down which files to monitor from the named share.`,
			Type:         dal.StringType,
			Required:     true,
			DefaultValue: `all`,
		}, {
			Name:        `target_path`,
			Description: `The local filesystem path where downloaded data will be stored.`,
			Type:        dal.StringType,
			Required:    true,
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
