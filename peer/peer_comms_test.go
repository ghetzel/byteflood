package peer

import (
	// "bytes"
	"crypto/rand"
	// "crypto/sha256"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/nacl/box"
	// "io"
	// "sync"
	"testing"
)

func makePeerPair() (peer1 *LocalPeer, peer2 *LocalPeer) {
	if publicKey, privateKey, err := box.GenerateKey(rand.Reader); err == nil {
		if localPeer, err := CreatePeer(
			`00000000-0000-0000-0000-111111111111`,
			[]byte(publicKey[:]),
			[]byte(privateKey[:]),
		); err == nil {
			localPeer.AutoReceiveMessages = false
			peer1 = localPeer
		} else {
			panic(err)
		}
	} else {
		panic(err)
	}

	if publicKey, privateKey, err := box.GenerateKey(rand.Reader); err == nil {
		if localPeer, err := CreatePeer(
			`00000000-0000-0000-0000-222222222222`,
			[]byte(publicKey[:]),
			[]byte(privateKey[:]),
		); err == nil {
			localPeer.AutoReceiveMessages = false
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

func TestPeerSmallTransfer(t *testing.T) {
	assert := require.New(t)
	peer1, peer2 := makePeerPair()

	// peer1 connects to peer2
	toPeer2fromPeer1, err := peer1.ConnectTo(peer2.Address, peer2.Port)
	assert.Nil(err)

	// peer2's view of peer1 (who just connected)
	toPeer1fromPeer2, ok := peer2.GetPeer(peer1.ID())
	assert.True(ok)
	assert.NotNil(toPeer1fromPeer2)

	// peer1 sends 0x42 to peer2
	_, err = toPeer2fromPeer1.SendMessage(NewMessage(DataBlock, []byte{0x42}))
	assert.Nil(err)

	// peer2 receives 0x42 from peer1
	msg, err := toPeer1fromPeer2.ReceiveMessage()
	assert.Nil(err)
	assert.NotNil(msg)
	assert.Equal([]byte{0x42}, msg.Data)

	// peer2 sends 0x41 to peer1
	_, err = toPeer1fromPeer2.Write([]byte{0x41})
	assert.Nil(err)

	// peer1 receives 0x41 from peer2
	msg, err = toPeer2fromPeer1.ReceiveMessage()
	assert.Nil(err)
	assert.NotNil(msg)
	assert.Equal([]byte{0x41}, msg.Data)
}

// func TestPeerCheckedTransfer(t *testing.T) {
// 	assert := require.New(t)
// 	peer1messages := make([]PeerMessage, 0)

// 	var err error
// 	var wg sync.WaitGroup
// 	peer1, peer2 := makePeerPair()

// 	toPeer1fromPeer2, err := peer2.ConnectTo(peer1.Address, peer1.Port)
// 	assert.Nil(err)

// 	go messagePump(peer1, &peer1messages, &wg, DataProceed, DataFinalize, DataFailed)

// 	// generate and checksum ~10MB of data
// 	data := make([]byte, (1024*1024*10)+773)
// 	rand.Read(data)
// 	sum := sha256.Sum256(data)

// 	err = toPeer1fromPeer2.BeginChecked(len(data))

// 	assert.Nil(err)

// 	_, err = io.Copy(toPeer1fromPeer2, bytes.NewBuffer(data))
// 	assert.Nil(err)

// 	err = toPeer1fromPeer2.FinishChecked()
// 	assert.Nil(err)

// 	wg.Wait()

// 	// verify DataStart message
// 	assert.Equal(3, len(peer1messages))
// 	assert.Equal(DataStart, peer1messages[0].Message.Type)
// 	assert.Equal(BinaryLEUint64, peer1messages[0].Message.Encoding)
// 	assert.Equal(uint64(len(data)), peer1messages[0].Message.Value())

// 	// verify DataFinalize message
// 	assert.Equal(DataFinalize, peer1messages[len(peer1messages)-1].Message.Type)
// 	assert.Equal(RawEncoding, peer1messages[len(peer1messages)-1].Message.Encoding)
// 	assert.Equal([]byte(sum[:]), peer1messages[len(peer1messages)-1].Message.Value())
// }

// func TestPeerLongCheckedTransfer(t *testing.T) {
// 	assert := require.New(t)
// 	peer1messages := make([]PeerMessage, 0)

// 	var err error
// 	var wg sync.WaitGroup
// 	peer1, peer2 := makePeerPair()

// 	fromPeer1, err := peer2.ConnectTo(peer1.Address, peer1.Port)
// 	assert.Nil(err)

// 	wg.Add(1)
// 	go messagePump(peer1, &peer1messages, &wg, DataFinalize, DataFailed)

// 	// generate and checksum ~10MB of data
// 	data := make([]byte, (1024 * 1024 * 4))
// 	rand.Read(data)

// 	// deliberately declare a short message header
// 	err = fromPeer1.BeginChecked((1024 * 1024 * 3))
// 	assert.Nil(err)

// 	// copy too much data (failure should be automatically sent from here)
// 	_, err = io.Copy(fromPeer1, bytes.NewBuffer(data))
// 	assert.NotNil(err)

// 	wg.Wait()

// 	// verify DataStart message
// 	assert.Equal(DataStart, peer1messages[0].Message.Type)

// 	// verify that we got a DataFailed message
// 	assert.Equal(DataFailed, peer1messages[len(peer1messages)-1].Message.Type)
// 	assert.Equal(StringEncoding, peer1messages[len(peer1messages)-1].Message.Encoding)

// 	// check error message
// 	msg, ok := peer1messages[len(peer1messages)-1].Message.Value().(string)
// 	assert.True(ok)
// 	log.Debugf("DataFailed: %v", msg)
// 	assert.True(len(msg) > 0)
// }

// func TestPeerShortCheckedTransfer(t *testing.T) {
// 	assert := require.New(t)
// 	peer1messages := make([]PeerMessage, 0)

// 	var err error
// 	var wg sync.WaitGroup
// 	peer1, peer2 := makePeerPair()

// 	fromPeer1, err := peer2.ConnectTo(peer1.Address, peer1.Port)
// 	assert.Nil(err)

// 	wg.Add(1)
// 	go messagePump(peer1, &peer1messages, &wg, DataFinalize, DataFailed)

// 	// generate and checksum ~10MB of data
// 	data := make([]byte, (1024 * 1024 * 4))
// 	rand.Read(data)

// 	// deliberately declare a short message header
// 	err = fromPeer1.BeginChecked(len(data))
// 	assert.Nil(err)

// 	// copy not enough data
// 	_, err = io.CopyN(fromPeer1, bytes.NewBuffer(data), int64(1024*1024*3))
// 	assert.Nil(err)

// 	// say we're finished too early
// 	err = fromPeer1.FinishChecked()
// 	assert.Nil(err)

// 	wg.Wait()

// 	// verify DataStart message
// 	assert.Equal(DataStart, peer1messages[0].Message.Type)

// 	// verify that we got a DataFailed message
// 	assert.Equal(DataFailed, peer1messages[len(peer1messages)-1].Message.Type)
// 	assert.Equal(StringEncoding, peer1messages[len(peer1messages)-1].Message.Encoding)

// 	// check error message
// 	msg, ok := peer1messages[len(peer1messages)-1].Message.Value().(string)
// 	assert.True(ok)
// 	log.Debugf("DataFailed: %v", msg)
// 	assert.True(len(msg) > 0)
// }

// func TestPeerTransact(t *testing.T) {
// 	assert := require.New(t)

// 	var err error
// 	peer1, peer2 := makePeerPair()

// 	peer2fromPeer1, err := peer1.ConnectTo(peer2.Address, peer2.Port)
// 	assert.Nil(err)

// 	send := []byte{0x99}
// 	recv := []byte{0x77}

// 	// peer1 sends message to peer2
// 	err = peer2fromPeer1.WriteChecked(send)
// 	assert.Nil(err)

// 	// peer2 reads what peer1 sent
// 	message := <-peer2.Messages
// 	data, err := message.FromPeer.ReadChecked()
// 	assert.Equal(send, data)

// 	// peer2 sends response
// 	message.FromPeer.WriteChecked(recv)

// 	// peer1 reads what peer2 replied with
// 	message = <-peer1.Messages
// 	data, err = message.FromPeer.ReadChecked()
// 	assert.Equal(recv, data)
// }
