package peer

import (
	"crypto/rand"
	"crypto/sha256"
	"github.com/ghetzel/byteflood/db"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/nacl/box"
	"io/ioutil"
	"os"
	"sync"
	"testing"
	"time"
)

var database *db.Database

func TestMain(m *testing.M) {
	if tmpfile, err := ioutil.TempFile(``, `byteflood`); err == nil {
		defer os.Remove(tmpfile.Name())

		database = db.NewDatabase()
		database.URI = `sqlite://` + tmpfile.Name()
		database.Indexer = ``

		if err := database.Initialize(); err == nil {
			os.Exit(m.Run())
		} else {
			panic(err.Error())
		}
	} else {
		panic(err.Error())
	}
}

func makePeerPair() (peer1 *LocalPeer, peer2 *LocalPeer) {
	if publicKey, privateKey, err := box.GenerateKey(rand.Reader); err == nil {
		localPeer := NewLocalPeer(database)
		localPeer.PublicKey = []byte(publicKey[:])
		localPeer.PrivateKey = []byte(privateKey[:])

		if err := localPeer.Initialize(); err == nil {
			localPeer.autoReceiveMessages = true
			peer1 = localPeer
		} else {
			panic(err)
		}
	} else {
		panic(err)
	}

	if publicKey, privateKey, err := box.GenerateKey(rand.Reader); err == nil {
		localPeer := NewLocalPeer(database)
		localPeer.PublicKey = []byte(publicKey[:])
		localPeer.PrivateKey = []byte(privateKey[:])

		if err := localPeer.Initialize(); err == nil {
			localPeer.autoReceiveMessages = true
			peer2 = localPeer
		} else {
			panic(err)
		}
	} else {
		panic(err)
	}

	// make peer2 known to peer1 (peer2 may initiate connections to peer1)
	peer1.AddAuthorizedPeer(peer2.ID(), `peer2`)
	peer2.AddAuthorizedPeer(peer1.ID(), `peer1`)

	go func() {
		if err := peer1.Run(); err != nil {
			panic(err)
		}
	}()

	go func() {
		if err := peer2.Run(); err != nil {
			panic(err)
		}
	}()

	// waits for both peers to declare that they're ready and listening
	for i := 0; i < 2; i++ {
		select {
		case <-peer1.WaitListen():
			continue
		case <-peer2.WaitListen():
			continue
		}
	}

	return
}

func TestPeerCheckedTransfer(t *testing.T) {
	assert := require.New(t)
	peer1, peer2 := makePeerPair()
	messages := make([]*Message, 0)
	var msglock sync.RWMutex

	// peer1 connects to peer2
	peer2fromPeer1, err := peer1.ConnectTo(peer2.Address)
	assert.Nil(err)

	time.Sleep(time.Second)

	// peer2's view of peer1 (who just connected)
	p1 := peer2.GetPeersByKey(peer1.GetPublicKey())
	assert.Equal(1, len(p1))
	peer1fromPeer2 := p1[0]
	assert.NotNil(peer1fromPeer2)

	// receive incoming messages
	peer1fromPeer2.SetMessageHandler(func(message *Message) {
		msglock.Lock()
		messages = append(messages, message)
		msglock.Unlock()
	})

	// declare that we will receive a new transfer
	transfer := peer1fromPeer2.CreateInboundTransfer(2048)

	// this is the data we want to send
	data := make([]byte, 2048)
	rand.Read(data)
	sum := sha256.Sum256(data)

	go func() {
		// peer1 sends data to peer2
		err = peer2fromPeer1.TransferData(transfer.ID, data)
		assert.Nil(err)
	}()

	time.Sleep(time.Second)

	msglock.RLock()
	assert.Equal(3, len(messages))

	var header, block, trailer *Message

	header = messages[0]
	block = messages[1]
	trailer = messages[2]
	msglock.RUnlock()

	// peer2: verify the header
	assert.NotNil(header)
	assert.Equal(transfer.ID, header.GroupID)
	assert.Equal(DataStart, header.Type)
	assert.Equal(BinaryLEUint64, header.Encoding)
	assert.Equal(uint64(len(data)), header.Value())

	// peer2: validate data block, trailer, and final ack
	assert.NotNil(block)
	assert.Equal(transfer.ID, block.GroupID)
	assert.Equal(DataBlock, block.Type)
	assert.Equal(RawEncoding, block.Encoding)
	assert.Equal(2048, len(block.Data))

	assert.NotNil(trailer)
	assert.Equal(transfer.ID, trailer.GroupID)
	assert.Equal(DataFinalize, trailer.Type)
	assert.Equal(RawEncoding, trailer.Encoding)
	assert.Equal([]byte(sum[:]), trailer.Value())
}
