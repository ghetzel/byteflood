package peer

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/nacl/box"
	"io"
	"sync"
	"testing"
)

func makePeerPair() (peer1 *LocalPeer, peer2 *LocalPeer) {
	if publicKey, privateKey, err := box.GenerateKey(rand.Reader); err == nil {
		if localPeer, err := CreatePeer(
			`11111111-1111-1111-1111-111111111111`,
			[]byte(publicKey[:]),
			[]byte(privateKey[:]),
		); err == nil {
			peer1 = localPeer
		} else {
			panic(err)
		}
	} else {
		panic(err)
	}

	if publicKey, privateKey, err := box.GenerateKey(rand.Reader); err == nil {
		if localPeer, err := CreatePeer(
			`22222222-2222-2222-2222-222222222222`,
			[]byte(publicKey[:]),
			[]byte(privateKey[:]),
		); err == nil {
			peer2 = localPeer
		} else {
			panic(err)
		}
	} else {
		panic(err)
	}

	go func() {
		if err := peer1.Listen(); err != nil {
			panic(err)
		}
	}()

	go func() {
		if err := peer2.Listen(); err != nil {
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

func messagePump(peer *LocalPeer, messages *[]PeerMessage, wg *sync.WaitGroup, mt ...MessageType) {
	for m := range peer.Messages {
		(*messages) = append(*messages, m)

		if wg != nil {
			for _, t := range mt {
				if m.Message.Type == t {
					wg.Done()
					return
				}
			}
		}
	}
}

func TestPeerSmallTransfer(t *testing.T) {
	assert := require.New(t)
	peer1messages := make([]PeerMessage, 0)
	peer2messages := make([]PeerMessage, 0)

	var err error
	var wg1, wg2 sync.WaitGroup
	peer1, peer2 := makePeerPair()

	fromPeer2, err := peer1.ConnectTo(peer2.Address, peer2.Port)
	assert.Nil(err)

	fromPeer1, err := peer2.ConnectTo(peer1.Address, peer1.Port)
	assert.Nil(err)

	wg1.Add(1)
	go messagePump(peer1, &peer1messages, &wg1, DataBlock)

	wg2.Add(1)
	go messagePump(peer2, &peer2messages, &wg2, DataBlock)

	fromPeer2.SendMessage(NewMessage(DataBlock, []byte{0x42}))
	fromPeer1.Write([]byte{0x41})

	wg1.Wait()
	wg2.Wait()

	assert.Equal(1, len(peer1messages))
	assert.Equal(fromPeer2.ID(), peer1messages[0].FromPeer.ID())
	assert.Equal([]byte{0x41}, peer1messages[0].Message.Data)

	assert.Equal(1, len(peer2messages))
	assert.Equal(fromPeer1.ID(), peer2messages[0].FromPeer.ID())
	assert.Equal([]byte{0x42}, peer2messages[0].Message.Data)
}

func TestPeerCheckedTransfer(t *testing.T) {
	assert := require.New(t)
	peer1messages := make([]PeerMessage, 0)

	var err error
	var wg sync.WaitGroup
	peer1, peer2 := makePeerPair()

	fromPeer1, err := peer2.ConnectTo(peer1.Address, peer1.Port)
	assert.Nil(err)

	wg.Add(1)
	go messagePump(peer1, &peer1messages, &wg, DataFinalize, DataFailed)

	// generate and checksum ~10MB of data
	data := make([]byte, (1024*1024*10)+773)
	rand.Read(data)
	sum := sha256.Sum256(data)

	err = fromPeer1.BeginChecked(len(data))
	assert.Nil(err)

	_, err = io.Copy(fromPeer1, bytes.NewBuffer(data))
	assert.Nil(err)

	err = fromPeer1.FinishChecked()
	assert.Nil(err)

	wg.Wait()

	// verify DataStart message
	assert.Equal(3, len(peer1messages))
	assert.Equal(DataStart, peer1messages[0].Message.Type)
	assert.Equal(BinaryLEUint64, peer1messages[0].Message.Encoding)
	assert.Equal(uint64(len(data)), peer1messages[0].Message.Value())

	// verify DataFinalize message
	assert.Equal(DataFinalize, peer1messages[len(peer1messages)-1].Message.Type)
	assert.Equal(RawEncoding, peer1messages[len(peer1messages)-1].Message.Encoding)
	assert.Equal([]byte(sum[:]), peer1messages[len(peer1messages)-1].Message.Value())
}

func TestPeerLongCheckedTransfer(t *testing.T) {
	assert := require.New(t)
	peer1messages := make([]PeerMessage, 0)

	var err error
	var wg sync.WaitGroup
	peer1, peer2 := makePeerPair()

	fromPeer1, err := peer2.ConnectTo(peer1.Address, peer1.Port)
	assert.Nil(err)

	wg.Add(1)
	go messagePump(peer1, &peer1messages, &wg, DataFinalize, DataFailed)

	// generate and checksum ~10MB of data
	data := make([]byte, (1024 * 1024 * 4))
	rand.Read(data)

	// deliberately declare a short message header
	err = fromPeer1.BeginChecked((1024 * 1024 * 3))
	assert.Nil(err)

	// copy too much data (failure should be automatically sent from here)
	_, err = io.Copy(fromPeer1, bytes.NewBuffer(data))
	assert.NotNil(err)

	wg.Wait()

	// verify DataStart message
	assert.Equal(DataStart, peer1messages[0].Message.Type)

	// verify that we got a DataFailed message
	assert.Equal(DataFailed, peer1messages[len(peer1messages)-1].Message.Type)
	assert.Equal(StringEncoding, peer1messages[len(peer1messages)-1].Message.Encoding)

	// check error message
	msg, ok := peer1messages[len(peer1messages)-1].Message.Value().(string)
	assert.True(ok)
	log.Debugf("DataFailed: %v", msg)
	assert.True(len(msg) > 0)
}

func TestPeerShortCheckedTransfer(t *testing.T) {
	assert := require.New(t)
	peer1messages := make([]PeerMessage, 0)

	var err error
	var wg sync.WaitGroup
	peer1, peer2 := makePeerPair()

	fromPeer1, err := peer2.ConnectTo(peer1.Address, peer1.Port)
	assert.Nil(err)

	wg.Add(1)
	go messagePump(peer1, &peer1messages, &wg, DataFinalize, DataFailed)

	// generate and checksum ~10MB of data
	data := make([]byte, (1024 * 1024 * 4))
	rand.Read(data)

	// deliberately declare a short message header
	err = fromPeer1.BeginChecked(len(data))
	assert.Nil(err)

	// copy not enough data
	_, err = io.CopyN(fromPeer1, bytes.NewBuffer(data), int64(1024*1024*3))
	assert.Nil(err)

	// say we're finished too early
	err = fromPeer1.FinishChecked()
	assert.Nil(err)

	wg.Wait()

	// verify DataStart message
	assert.Equal(DataStart, peer1messages[0].Message.Type)

	// verify that we got a DataFailed message
	assert.Equal(DataFailed, peer1messages[len(peer1messages)-1].Message.Type)
	assert.Equal(StringEncoding, peer1messages[len(peer1messages)-1].Message.Encoding)

	// check error message
	msg, ok := peer1messages[len(peer1messages)-1].Message.Value().(string)
	assert.True(ok)
	log.Debugf("DataFailed: %v", msg)
	assert.True(len(msg) > 0)
}
