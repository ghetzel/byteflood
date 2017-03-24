package byteflood

import (
	"github.com/ghetzel/byteflood/db"
	"time"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func setupApplication(assert *require.Assertions, source string, dest string) *Application {
	app := NewApplication()
	app.LocalPeer.Address = `127.0.0.1:0`
	app.API.Address = `127.0.0.1:0`

	var basedir string

	if p, err := ioutil.TempDir(``, `byteflood_test_`); err == nil {
		basedir = p
	} else {
		assert.Nil(err)
	}

	assert.Nil(os.Mkdir(filepath.Join(basedir, `keys`), 0700))

	pubkeyPath := filepath.Join(basedir, `keys`, `peer.pub`)
	privkeyPath := filepath.Join(basedir, `keys`, `peer.key`)

	assert.Nil(encryption.GenerateKeypair(pubkeyPath, privkeyPath))

	app.PublicKeyPath = pubkeyPath
	app.PrivateKeyPath = privkeyPath
	app.Database.BaseDirectory = basedir
	app.Database.ExtractFields = []string{
		`/music/(?P<media__artist>[^/]+)/(?P<media__album>[^/]+)/(?P<media__track>\d+) (?P<media__title>[^\.]*)\.`,
	}

	assert.Nil(app.Initialize())

	dir, err := filepath.Abs(filepath.Join(source, `music`))
	assert.Nil(err)
	info, err := os.Stat(dir)
	assert.Nil(err)
	assert.True(info.IsDir())

	assert.Nil(app.Database.ScannedDirectories.Create(db.NewDirectory(app.Database, dir)))
	assert.Nil(app.Database.Shares.Create(&shares.Share{
		ID:         `music`,
		BaseFilter: `label=music`,
	}))

	sub := NewSubscription(1, `music`, ``, filepath.Join(dest, `music`))
	assert.Nil(app.Database.Subscriptions.Create(sub))

	return app
}

func TestSingleApplication(t *testing.T) {
	assert := require.New(t)
	app := setupApplication(assert, `./tests/files`, `./tests/target`)

	defer func(){
		os.RemoveAll(app.Database.BaseDirectory)
		os.RemoveAll(`./tests/target`)
	}()

	// Scan Testing
	// ------------------------------------------------------------------------
	assert.Nil(app.Scan(true))

	data, err := app.Database.Metadata.List([]string{
		`metadata.media.album`,
		`metadata.media.artist`,
	})
	assert.Nil(err)

	// verify the scan imported the data we're expecting
	v, ok := data[`metadata.media.album`]
	assert.True(ok)
	albums, err := stringutil.ToStringSlice(v)
	assert.Nil(err)
	sort.Strings(albums)
	assert.Equal([]string{
		`a night at the roxbury`,
		`arrival`,
	}, albums)

	v, ok = data[`metadata.media.artist`]
	assert.True(ok)
	artists, err := stringutil.ToStringSlice(v)
	assert.Nil(err)
	sort.Strings(artists)
	assert.Equal([]string{
		`abba`,
		`various artists`,
	}, artists)

	// Share Testing
	// ------------------------------------------------------------------------
	musicShare := new(shares.Share)
	assert.Nil(app.Database.Shares.Get(`music`, musicShare))
	assert.Equal(`music`, musicShare.ID)
	assert.Equal(28, musicShare.Length())

	file, err := musicShare.Get(`fvtxmdq6mfisi`)
	assert.Nil(err)
	assert.Equal(`fvtxmdq6mfisi`, file.ID)
	assert.Equal(`ppe74ow3qnxpm`, file.Parent)
	assert.Equal(`/ABBA/Arrival/02 Dancing Queen.mp3`, file.RelativePath)
	assert.Equal(`Dancing Queen`, file.Get(`media.title`))

	recordset, err := musicShare.Find(`metadata.media.title=contains:love`, 50, 0, nil)
	assert.Nil(err)
	assert.Equal(4, int(recordset.ResultCount))
	assert.Len(recordset.Records, 4)
}

func TestApplicationSuperFriends(t *testing.T) {
	assert := require.New(t)

	local := setupApplication(assert, `./tests/files`, `./tests/target`)
	friend := setupApplication(assert, `./tests/files`, `./tests/friend`)

	// build databases
	assert.Nil(local.Scan(true))
	assert.Nil(friend.Scan(true))

	defer func(){
		os.RemoveAll(local.Database.BaseDirectory)
		os.RemoveAll(friend.Database.BaseDirectory)
		os.RemoveAll(`./tests/target`)
		os.RemoveAll(`./tests/friend`)
	}()

	// authorize local->friend
	assert.Nil(local.Database.AuthorizedPeers.Create(&peer.AuthorizedPeer{
		ID: friend.LocalPeer.ID(),
		PeerName: `friend`,
	}))

	// authorize friend->local
	assert.Nil(friend.Database.AuthorizedPeers.Create(&peer.AuthorizedPeer{
		ID: local.LocalPeer.ID(),
		PeerName: `local`,
	}))

	// start
	go local.Run()
	time.Sleep(time.Second)

	go friend.Run()
	time.Sleep(time.Second)

	// connect
	_, err := local.LocalPeer.ConnectTo(friend.LocalPeer.Address)
	assert.Nil(err)

	// Subscription Testing
	// ------------------------------------------------------------------------
	musicSub := new(Subscription)
	assert.Nil(local.Database.Subscriptions.Get(1, musicSub))
	assert.Equal(1, musicSub.ID)
	wanted, err := musicSub.GetWantedItems(local)
	assert.Nil(err)
	assert.Len(wanted, 24)
}
