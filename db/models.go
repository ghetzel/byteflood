package db

import (
	"github.com/ghetzel/pivot/dal"
	"github.com/ghetzel/pivot/mapper"
)

var Metadata mapper.Mapper
var Shares mapper.Mapper
var Downloads mapper.Mapper
var AuthorizedPeers mapper.Mapper
var System mapper.Mapper
var ScannedDirectories mapper.Mapper
var Subscriptions mapper.Mapper

var Schema = map[string]*dal.Collection{
	`shares`:              SharesSchema,
	`metadata`:            MetadataSchema,
	`downloads`:           DownloadsSchema,
	`authorized_peers`:    AuthorizedPeersSchema,
	`system`:              SystemSchema,
	`scanned_directories`: ScannedDirectoriesSchema,
	`subscriptions`:       SubscriptionsSchema,
	// aliases
	`directories`: ScannedDirectoriesSchema,
	`peers`:       AuthorizedPeersSchema,
	`properties`:  SystemSchema,
}

var Models = map[string]mapper.Mapper{}
