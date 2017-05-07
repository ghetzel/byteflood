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
	`directories`:   ScannedDirectoriesSchema,
	`downloads`:     DownloadsSchema,
	`peers`:         AuthorizedPeersSchema,
	`shares`:        SharesSchema,
	`subscriptions`: SubscriptionsSchema,
	`properties`:    SystemSchema,
	`metadata`:      MetadataSchema,
}

var Models = map[string]mapper.Mapper{}
