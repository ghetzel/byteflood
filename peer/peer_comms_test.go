package peer

import (
	// "bytes"
	"crypto/rand"
	"crypto/sha256"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/nacl/box"
	// "io"
	"testing"
	"time"
)

func makePeerPair() (peer1 *LocalPeer, peer2 *LocalPeer) {
	if publicKey, privateKey, err := box.GenerateKey(rand.Reader); err == nil {
		localPeer := NewLocalPeer()
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
		localPeer := NewLocalPeer()
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
	peer1.AddKnownPeer(`peer2`, peer2.ID())
	peer2.AddKnownPeer(`peer1`, peer1.ID())

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

func _TestPeerSmallTransfer(t *testing.T) {
	assert := require.New(t)
	peer1, peer2 := makePeerPair()

	// peer1 connects to peer2
	peer2fromPeer1, err := peer1.ConnectTo(peer2.Address)
	assert.Nil(err)

	// peer2's view of peer1 (who just connected)
	p1 := peer2.GetPeersByKey(peer1.GetPublicKey())
	assert.Equal(1, len(p1))
	peer1fromPeer2 := p1[0]

	assert.NotNil(peer1fromPeer2)

	// peer1 sends 0x42 to peer2, peer2 receives 0x42 reply from peer1
	msg, err := peer2fromPeer1.SendMessageChecked(NewMessage(DataBlock, []byte{0x42}), time.Second)
	assert.Nil(err)
	assert.NotNil(msg)
	assert.Equal([]byte{0x42}, msg.Data)

	// peer2 sends 0x41 to peer1
	_, err = peer1fromPeer2.Write([]byte{0x41})
	assert.Nil(err)

	// peer1 receives 0x41 from peer2
	// msg, err = peer2fromPeer1.WaitNextMessage(time.Second)
	// assert.Nil(err)
	// assert.NotNil(msg)
	// assert.Equal([]byte{0x41}, msg.Data)
}

func TestPeerCheckedTransfer(t *testing.T) {
	assert := require.New(t)
	peer1, peer2 := makePeerPair()
	messages := make([]*Message, 0)

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
		messages = append(messages, message)
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

	assert.Equal(3, len(messages))

	var header, block, trailer *Message

	header = messages[0]
	block = messages[1]
	trailer = messages[2]

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

// func TestPeerLongCheckedTransfer(t *testing.T) {
// 	assert := require.New(t)
// 	peer1messages := make([]PeerMessage, 0)

// 	var err error
// 	var wg sync.WaitGroup
// 	peer1, peer2 := makePeerPair()

// 	fromPeer1, err := peer2.ConnectTo(peer1.Address)
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

// 	fromPeer1, err := peer2.ConnectTo(peer1.Address)
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

// 	peer2fromPeer1, err := peer1.ConnectTo(peer2.Address)
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
