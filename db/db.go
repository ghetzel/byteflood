package db

import (
	"github.com/ghetzel/metabase"
	"github.com/ghetzel/pivot/mapper"
)

var Instance *metabase.DB

func GetScannedDirectories() ([]metabase.Group, error) {
	var dirs []metabase.Group

	if err := ScannedDirectories.All(&dirs); err == nil {
		return dirs, nil
	} else {
		return nil, err
	}
}

func SetupSchemata(db *metabase.DB) error {
	pivotBackend := db.GetPivotDatabase()

	// register global mapper.Model instances to this database
	AuthorizedPeers = mapper.NewModel(pivotBackend, AuthorizedPeersSchema)
	Downloads = mapper.NewModel(pivotBackend, DownloadsSchema)
	ScannedDirectories = mapper.NewModel(pivotBackend, ScannedDirectoriesSchema)
	Shares = mapper.NewModel(pivotBackend, SharesSchema)
	Subscriptions = mapper.NewModel(pivotBackend, SubscriptionsSchema)
	System = mapper.NewModel(pivotBackend, SystemSchema)

	Models[`shares`] = Shares
	Models[`metadata`] = metabase.Metadata
	Models[`downloads`] = Downloads
	Models[`authorized_peers`] = AuthorizedPeers
	Models[`system`] = System
	Models[`scanned_directories`] = ScannedDirectories
	Models[`subscriptions`] = Subscriptions

	// aliases
	Models[`directories`] = ScannedDirectories
	Models[`peers`] = AuthorizedPeers
	Models[`properties`] = System

	// set global default DB instance to us
	Instance = db

	models := []mapper.Mapper{
		metabase.Metadata,
		AuthorizedPeers,
		Downloads,
		ScannedDirectories,
		Shares,
		Subscriptions,
		System,
	}

	for _, model := range models {
		if err := model.Migrate(); err != nil {
			return err
		}
	}

	db.GroupLister = GetScannedDirectories
	// db.Initializer

	return nil
}
