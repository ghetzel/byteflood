package byteflood

import (
	"github.com/ghetzel/byteflood/db"
	// "github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/op/go-logging"
	"github.com/stretchr/testify/require"
	"os"
	"sort"
	"testing"
	// "time"
)

func TestMain(m *testing.M) {
	// turn down aggressively-verbose logging
	logging.SetLevel(logging.ERROR, `diecast`)
	logging.SetLevel(logging.CRITICAL, `pivot/querylog`)

	os.Exit(m.Run())
}

func TestSingleApplication(t *testing.T) {
	os.RemoveAll(`./tests`)
	assert := require.New(t)
	assert.NoError(generateTestDirectory())
	app := setupApplication(assert, `./tests/files`, `./tests/target`)

	os.MkdirAll(`./tests/files/music`, 0700)

	defer func() {
		os.RemoveAll(app.Database.BaseDirectory)
		os.RemoveAll(`./tests`)
	}()

	// Scan Testing
	// ------------------------------------------------------------------------
	assert.NoError(app.Scan(true))

	data, err := db.Metadata.List([]string{
		`metadata.media.album`,
		`metadata.media.artist`,
	})
	assert.NoError(err)

	// verify the scan imported the data we're expecting
	v, ok := data[`metadata.media.album`]
	assert.True(ok)
	albums, err := stringutil.ToStringSlice(v)
	assert.NoError(err)
	sort.Strings(albums)
	assert.Equal([]string{
		`a night at the roxbury`,
		`arrival`,
	}, albums)

	v, ok = data[`metadata.media.artist`]
	assert.True(ok)
	artists, err := stringutil.ToStringSlice(v)
	assert.NoError(err)
	sort.Strings(artists)
	assert.Equal([]string{
		`abba`,
		`various artists`,
	}, artists)

	// Share Testing
	// ------------------------------------------------------------------------
	musicShare, ok := db.SharesSchema.NewInstance().(*shares.Share)
	assert.True(ok)
	assert.NoError(db.Shares.Get(`music`, musicShare))
	assert.Equal(`music`, musicShare.ID)
	// disabled because stats is shared among all instances...
	// assert.Equal(28, musicShare.Length())

	file, err := musicShare.Get(`fvtxmdq6mfisi`)
	assert.NoError(err)
	assert.Equal(`fvtxmdq6mfisi`, file.ID)
	assert.Equal(`ppe74ow3qnxpm`, file.Parent)
	assert.Equal(`/ABBA/Arrival/02 Dancing Queen.mp3`, file.RelativePath)
	assert.Equal(`Dancing Queen`, file.Get(`media.title`))

	recordset, err := musicShare.Find(`metadata.media.title=contains:love`, 50, 0, nil, nil)
	assert.NoError(err)
	assert.Equal(4, int(recordset.ResultCount))
	assert.Len(recordset.Records, 4)

	// disabled because stats is shared among all instances...
	//
	// musicShareStats, err := musicShare.GetStats()
	// assert.NoError(err)
	// assert.Equal(int64(24), musicShareStats.FileCount)
	// assert.Equal(int64(4), musicShareStats.DirectoryCount)
	// assert.Equal(int64(12594), musicShareStats.TotalBytes)
}

// func TestApplicationSuperFriends(t *testing.T) {
// 	os.RemoveAll(`./tests`)
// 	assert := require.New(t)
// 	assert.NoError(generateTestDirectory())

// 	local := setupApplication(assert, `./tests/files`, `./tests/target`)
// 	friend := setupApplication(assert, `./tests/files`, `./tests/friend`)

// 	// build databases
// 	assert.NoError(local.Scan(true))
// 	assert.NoError(friend.Scan(true))

// 	defer func() {
// 		os.RemoveAll(local.Database.BaseDirectory)
// 		os.RemoveAll(friend.Database.BaseDirectory)
// 		os.RemoveAll(`./tests`)
// 	}()

// 	// authorize local->friend
// 	assert.NoError(local.Database.AuthorizedPeers.Create(&peer.AuthorizedPeer{
// 		ID:       friend.LocalPeer.GetID(),
// 		PeerName: `friend`,
// 	}))

// 	// authorize friend->local
// 	assert.NoError(friend.Database.AuthorizedPeers.Create(&peer.AuthorizedPeer{
// 		ID:       local.LocalPeer.GetID(),
// 		PeerName: `local`,
// 	}))

// 	// start
// 	go local.Run()
// 	time.Sleep(time.Second)

// 	go friend.Run()
// 	time.Sleep(time.Second)

// 	// connect
// 	_, err := local.LocalPeer.ConnectTo(friend.LocalPeer.Address)
// 	assert.NoError(err)

// 	// Subscription Testing
// 	// ------------------------------------------------------------------------
// 	musicSub := new(Subscription)
// 	assert.NoError(local.Database.Subscriptions.Get(1, musicSub))
// 	assert.Equal(1, musicSub.ID)

// 	// verify we need all the files
// 	wanted, err := musicSub.GetWantedItems(local.LocalPeer)
// 	assert.NoError(err)
// 	assert.Len(wanted, 24)

// 	// copy 3 files into the target directory to simulate already having them
// 	assert.NoError(os.MkdirAll(`./tests/target/music/ABBA/Arrival`, 0755))
// 	assert.NoError(copyFile(
// 		`./tests/files/music/ABBA/Arrival/01 When I Kissed the Teacher.mp3`,
// 		`./tests/target/music/ABBA/Arrival/01 When I Kissed the Teacher.mp3`,
// 		false,
// 	))

// 	assert.NoError(copyFile(
// 		`./tests/files/music/ABBA/Arrival/02 Dancing Queen.mp3`,
// 		`./tests/target/music/ABBA/Arrival/02 Dancing Queen.mp3`,
// 		false,
// 	))

// 	assert.NoError(copyFile(
// 		`./tests/files/music/ABBA/Arrival/03 Dum Dum Diddle.mp3`,
// 		`./tests/target/music/ABBA/Arrival/03 Dum Dum Diddle.mp3`,
// 		false,
// 	))

// 	// verify we need 3 fewer files
// 	wanted, err = musicSub.GetWantedItems(local.LocalPeer)
// 	assert.NoError(err)
// 	assert.Len(wanted, 21)

// 	// perform a sync to transfer the rest
// 	assert.NoError(musicSub.Sync(local))

// 	time.Sleep(10 * time.Second)

// 	// verify we need don't need files anymore
// 	wanted, err = musicSub.GetWantedItems(local.LocalPeer)
// 	assert.NoError(err)
// 	assert.Empty(wanted)
// }
