package byteflood

import (
	"crypto/rand"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/op/go-logging"
	"github.com/stretchr/testify/require"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

var testMusicDirectory = map[string]int64{
	`./tests/files/music/ABBA/Arrival/01 When I Kissed the Teacher.mp3`:                                               180,
	`./tests/files/music/ABBA/Arrival/02 Dancing Queen.mp3`:                                                           230,
	`./tests/files/music/ABBA/Arrival/03 Dum Dum Diddle.mp3`:                                                          170,
	`./tests/files/music/ABBA/Arrival/04 My Love, My Life.mp3`:                                                        232,
	`./tests/files/music/ABBA/Arrival/05 Tiger.mp3`:                                                                   175,
	`./tests/files/music/ABBA/Arrival/06 Money, Money, Money.mp3`:                                                     185,
	`./tests/files/music/ABBA/Arrival/07 That's Me.mp3`:                                                               195,
	`./tests/files/music/ABBA/Arrival/08 Why Did It Have to Be Me.mp3`:                                                200,
	`./tests/files/music/ABBA/Arrival/09 Knowing Me, Knowing You.mp3`:                                                 242,
	`./tests/files/music/ABBA/Arrival/10 Arrival.mp3`:                                                                 180,
	`./tests/files/music/Various Artists/A Night at the Roxbury/01 What Is Love (7' Mix).flac`:                        101,
	`./tests/files/music/Various Artists/A Night at the Roxbury/02 Bamboogie (Radio Edit).flac`:                       202,
	`./tests/files/music/Various Artists/A Night at the Roxbury/03 Make That Money (Roxbury Remix).flac`:              303,
	`./tests/files/music/Various Artists/A Night at the Roxbury/04 Disco Inferno.flac`:                                404,
	`./tests/files/music/Various Artists/A Night at the Roxbury/05 Da Ya Think I'm Sexy (Featuring Rod Stewart).flac`: 505,
	`./tests/files/music/Various Artists/A Night at the Roxbury/06 Pop Muzik.flac`:                                    606,
	`./tests/files/music/Various Artists/A Night at the Roxbury/07 Insomia (Monster Mix).flac`:                        707,
	`./tests/files/music/Various Artists/A Night at the Roxbury/08 Be My Lover (Club Mix).flac`:                       808,
	`./tests/files/music/Various Artists/A Night at the Roxbury/09 This Is Your Night.flac`:                           909,
	`./tests/files/music/Various Artists/A Night at the Roxbury/10 Beautiful Life.flac`:                               1010,
	`./tests/files/music/Various Artists/A Night at the Roxbury/11 Where Do You Go (Ocean Drive Mix).flac`:            1111,
	`./tests/files/music/Various Artists/A Night at the Roxbury/12 A Little Bit Of Ecstacy.flac`:                      1212,
	`./tests/files/music/Various Artists/A Night at the Roxbury/13 What Is Love (Refreshmento Extro Radio Mix).flac`:  1313,
	`./tests/files/music/Various Artists/A Night at the Roxbury/14 Careless Whisper.flac`:                             1414,
}

func TestMain(m *testing.M) {
	// turn down aggressively-verbose logging
	logging.SetLevel(logging.ERROR, `diecast`)
	logging.SetLevel(logging.CRITICAL, `pivot/querylog`)

	os.Exit(m.Run())
}

func copyFile(srcPath string, destPath string, randomlyCorruptItInTheProcess bool) error {
	if src, err := os.Open(srcPath); err == nil {
		if dest, err := os.OpenFile(destPath, (os.O_WRONLY | os.O_CREATE | os.O_TRUNC), 0666); err == nil {
			_, err := io.Copy(dest, src)
			return err
		} else {
			return err
		}
	} else {
		return err
	}
}

func generateTestFile(destPath string, length int64) error {
	if err := os.MkdirAll(path.Dir(destPath), 0755); err == nil {
		if dest, err := os.OpenFile(destPath, (os.O_WRONLY | os.O_CREATE | os.O_TRUNC), 0666); err == nil {
			_, err := io.CopyN(dest, rand.Reader, length)
			return err
		} else {
			return err
		}
	} else {
		return err
	}
}

func generateTestDirectory() error {
	os.RemoveAll(`./tests/files`)

	for name, size := range testMusicDirectory {
		if err := generateTestFile(name, size); err != nil {
			return err
		}
	}

	return nil
}

func setupApplication(assert *require.Assertions, source string, dest string) *Application {
	app := NewApplication()
	app.LocalPeer.Address = `127.0.0.1:0`
	app.API.Address = `127.0.0.1:0`

	var basedir string

	if p, err := ioutil.TempDir(``, `byteflood_test_`); err == nil {
		basedir = p
	} else {
		assert.NoError(err)
	}

	assert.NoError(os.Mkdir(filepath.Join(basedir, `keys`), 0700))

	pubkeyPath := filepath.Join(basedir, `keys`, `peer.pub`)
	privkeyPath := filepath.Join(basedir, `keys`, `peer.key`)

	assert.NoError(encryption.GenerateKeypair(pubkeyPath, privkeyPath))

	app.PublicKeyPath = pubkeyPath
	app.PrivateKeyPath = privkeyPath
	app.Database.BaseDirectory = basedir
	app.Database.ExtractFields = []string{
		`/music/(?P<media__artist>[^/]+)/(?P<media__album>[^/]+)/(?P<media__track>\d+) (?P<media__title>[^\.]*)\.`,
	}

	assert.NoError(app.Initialize())

	dir, err := filepath.Abs(filepath.Join(source, `music`))
	assert.NoError(err)
	info, err := os.Stat(dir)
	assert.NoError(err)
	assert.True(info.IsDir())

	directory, ok := app.Database.ScannedDirectories.NewInstance(func(i interface{}) interface{} {
		d := i.(*db.Directory)
		d.Path = dir
		return d
	}).(*db.Directory)

	assert.True(ok)
	assert.NoError(app.Database.ScannedDirectories.Create(directory))
	assert.NoError(app.Database.Shares.Create(&shares.Share{
		ID:         `music`,
		BaseFilter: `label=music`,
	}))

	sub := NewSubscription(1, `music`, ``, filepath.Join(dest, `music`))
	assert.NoError(app.Database.Subscriptions.Create(sub))

	return app
}

func TestSingleApplication(t *testing.T) {
	assert := require.New(t)
	assert.NoError(generateTestDirectory())
	app := setupApplication(assert, `./tests/files`, `./tests/target`)

	defer func() {
		os.RemoveAll(app.Database.BaseDirectory)
		os.RemoveAll(`./tests`)
	}()

	// Scan Testing
	// ------------------------------------------------------------------------
	assert.NoError(app.Scan(true))

	data, err := app.Database.Metadata.List([]string{
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
	assert.NoError(app.Database.Shares.Get(`music`, musicShare))
	assert.Equal(`music`, musicShare.ID)
	assert.Equal(28, musicShare.Length())

	file, err := musicShare.Get(`fvtxmdq6mfisi`)
	assert.NoError(err)
	assert.Equal(`fvtxmdq6mfisi`, file.ID)
	assert.Equal(`ppe74ow3qnxpm`, file.Parent)
	assert.Equal(`/ABBA/Arrival/02 Dancing Queen.mp3`, file.RelativePath)
	assert.Equal(`Dancing Queen`, file.Get(`media.title`))

	recordset, err := musicShare.Find(`metadata.media.title=contains:love`, 50, 0, nil)
	assert.NoError(err)
	assert.Equal(4, int(recordset.ResultCount))
	assert.Len(recordset.Records, 4)
}

func TestApplicationSuperFriends(t *testing.T) {
	assert := require.New(t)
	assert.NoError(generateTestDirectory())

	local := setupApplication(assert, `./tests/files`, `./tests/target`)
	friend := setupApplication(assert, `./tests/files`, `./tests/friend`)

	// build databases
	assert.NoError(local.Scan(true))
	assert.NoError(friend.Scan(true))

	defer func() {
		os.RemoveAll(local.Database.BaseDirectory)
		os.RemoveAll(friend.Database.BaseDirectory)
		os.RemoveAll(`./tests`)
	}()

	// authorize local->friend
	assert.NoError(local.Database.AuthorizedPeers.Create(&peer.AuthorizedPeer{
		ID:       friend.LocalPeer.ID(),
		PeerName: `friend`,
	}))

	// authorize friend->local
	assert.NoError(friend.Database.AuthorizedPeers.Create(&peer.AuthorizedPeer{
		ID:       local.LocalPeer.ID(),
		PeerName: `local`,
	}))

	// start
	go local.Run()
	time.Sleep(time.Second)

	go friend.Run()
	time.Sleep(time.Second)

	// connect
	_, err := local.LocalPeer.ConnectTo(friend.LocalPeer.Address)
	assert.NoError(err)

	// Subscription Testing
	// ------------------------------------------------------------------------
	musicSub := new(Subscription)
	assert.NoError(local.Database.Subscriptions.Get(1, musicSub))
	assert.Equal(1, musicSub.ID)

	// verify we need all the files
	wanted, err := musicSub.GetWantedItems(local.LocalPeer)
	assert.NoError(err)
	assert.Len(wanted, 24)

	// copy three files in outside of byteflood
	assert.NoError(os.MkdirAll(`./tests/target/music/ABBA/Arrival`, 0755))
	assert.NoError(copyFile(
		`./tests/files/music/ABBA/Arrival/01 When I Kissed the Teacher.mp3`,
		`./tests/target/music/ABBA/Arrival/01 When I Kissed the Teacher.mp3`,
		false,
	))

	assert.NoError(copyFile(
		`./tests/files/music/ABBA/Arrival/02 Dancing Queen.mp3`,
		`./tests/target/music/ABBA/Arrival/02 Dancing Queen.mp3`,
		false,
	))

	assert.NoError(copyFile(
		`./tests/files/music/ABBA/Arrival/03 Dum Dum Diddle.mp3`,
		`./tests/target/music/ABBA/Arrival/03 Dum Dum Diddle.mp3`,
		false,
	))

	// verify we need 3 fewer files
	wanted, err = musicSub.GetWantedItems(local.LocalPeer)
	assert.NoError(err)
	assert.Len(wanted, 21)

	// perform a sync to transfer the rest
	assert.NoError(musicSub.Sync(local))

	time.Sleep(10 * time.Second)

	// verify we need don't need files anymore
	wanted, err = musicSub.GetWantedItems(local.LocalPeer)
	assert.NoError(err)
	assert.Empty(wanted)
}
