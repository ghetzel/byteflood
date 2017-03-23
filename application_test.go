package byteflood

import (
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func setupApplication(assert *require.Assertions) *Application {
	app := &Application{
		LocalPeer: peer.NewLocalPeer(),
		Database:  db.NewDatabase(),
	}

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

	dir, err := filepath.Abs(`./tests/files/music`)
	assert.Nil(err)
	info, err := os.Stat(dir)
	assert.Nil(err)
	assert.True(info.IsDir())
	assert.Nil(db.ScannedDirectories.Create(db.NewDirectory(app.Database, dir)))

	return app
}

func TestSingleApplication(t *testing.T) {
	assert := require.New(t)
	app := setupApplication(assert)
	defer os.RemoveAll(app.Database.BaseDirectory)

	assert.Nil(app.Scan(true))

	data, err := db.Metadata.List([]string{
		`metadata.media.album`,
		`metadata.media.artist`,
	})
	assert.Nil(err)

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
}
