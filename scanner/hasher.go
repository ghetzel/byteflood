package scanner

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"github.com/jackpal/bencode-go"
	"io"
	"math"
	"os"
	"strings"
	"time"
)

const DEFAULT_BF_CREATOR = `byteflood`
const DEFAULT_BF_HASH_PIECELENGTH = 262144

type Epoch int

func (self Epoch) Time() time.Time {
	return time.Unix(int64(self), 0)
}

type TorrentFile struct {
	Path   []string `bencode:"path"`
	Length int      `bencode:"length"`
	MD5Sum string   `bencode:"md5sum,omitempty"`
}

type TorrentInfo struct {
	PieceLength int           `bencode:"piece length"`
	Pieces      []byte        `bencode:"pieces"`
	Private     int           `bencode:"private"`
	Name        string        `bencode:"name,omitempty"`
	Length      int           `bencode:"length,omitempty"`
	MD5Sum      string        `bencode:"md5sum,omitempty"`
	Files       []TorrentFile `bencode:"files,omitempty"`
}

type Torrent struct {
	Announce      string      `bencode:"announce"`
	AnnounceList  []string    `bencode:"announce-list,omitempty"`
	CreationEpoch Epoch       `bencode:"creation date,omitempty"`
	CreatedBy     string      `bencode:"created by,omitempty"`
	Info          TorrentInfo `bencode:"info"`
	hasOneFile    bool        `bencode:"-"`
}

func NewTorrent() *Torrent {
	return &Torrent{
		Info: TorrentInfo{
			PieceLength: DEFAULT_BF_HASH_PIECELENGTH,
			Pieces: make([]byte, 0),
			Private: 1,
		},
	}
}

func ReadTorrent(data []byte) (Torrent, error) {
	torrent := Torrent{}
	buffer := bytes.NewBuffer(data)

	if err := bencode.Unmarshal(buffer, &torrent); err == nil {
		return torrent, nil
	} else {
		return torrent, err
	}
}

func (self *Torrent) AddPiece(data []byte) {
	self.Info.Pieces = append(self.Info.Pieces, data...)
}

func (self *Torrent) PieceCount() (int, error) {
	chunks := float64(len(self.Info.Pieces) / sha1.Size)

	if chunks == float64(int(chunks)) {
		return int(chunks), nil
	}else{
		return -1, fmt.Errorf("Invalid chunk count: %f", chunks)
	}
}

func (self *Torrent) GetPieceSum(index int) ([]byte, bool) {
	offset := (index * sha1.Size)

	if (offset + sha1.Size) <= len(self.Info.Pieces) {
		return self.Info.Pieces[offset:(offset+sha1.Size)], true
	}

	return nil, false
}

func (self *Torrent) AddFile(name string, length int64) error {
	log.Debugf("Adding %s (%d bytes)", name, length)

	// first file gets placed in the top-level of the info struct
	if !self.hasOneFile {
		if path, err := self.PreparePath(name); err == nil {
			self.hasOneFile = true
			self.Info.Name = path[len(path)-1]
			self.Info.Length = int(length)
		} else {
			return err
		}
	} else {
		if self.Info.Files == nil {
			if path, err := self.PreparePath(self.Info.Name); err == nil {
				self.Info.Files = []TorrentFile{
					{
						Path:   path,
						Length: self.Info.Length,
						MD5Sum: self.Info.MD5Sum,
					},
				}

				self.Info.Name = ``
				self.Info.Length = 0
				self.Info.MD5Sum = ``
			} else {
				return err
			}
		}

		if path, err := self.PreparePath(name); err == nil {
			self.Info.Files = append(self.Info.Files, TorrentFile{
				Path:   path,
				Length: int(length),
			})
		} else {
			return err
		}
	}

	return nil
}

func (self *Torrent) Validate() error {
	var chunks int

	if self.Announce == `` {
		return fmt.Errorf("Torrent Infohash must specify at least one announce URL")
	}

	if self.Info.PieceLength == 0 {
		return fmt.Errorf("Torrent must specify a piece length")
	}

	if self.Info.Name == `` && (self.Info.Files == nil || len(self.Info.Files) == 0) {
		return fmt.Errorf("Torrent must contain at least one file")
	}

	// single-file validation
	if self.Info.Name != `` {
		chunks = int(math.Ceil(float64(self.Info.Length) / float64(self.Info.PieceLength)))
	} else {
		// multi-file validation
		for _, torrentFile := range self.Info.Files {
			chunks += int(math.Ceil(float64(torrentFile.Length) / float64(self.Info.PieceLength)))
		}
	}

	if shouldHave := (chunks * sha1.Size); shouldHave != len(self.Info.Pieces) {
		return fmt.Errorf("Invalid piece length: %d %d-byte chunks should produce %d pieces, have: %d", chunks, self.Info.PieceLength, shouldHave, len(self.Info.Pieces))
	}

	return nil
}

func (self *Torrent) SetPrivate(isPrivate bool) {
	if isPrivate {
		self.Info.Private = 1
	} else {
		self.Info.Private = 0
	}
}

func (self *Torrent) WriteTo(w io.Writer) error {
	if err := self.Validate(); err == nil {
		return bencode.Marshal(w, *self)
	} else {
		return err
	}
}

func (self *Torrent) PreparePath(path string) ([]string, error) {
	if strings.Contains(path, `..`) || strings.HasPrefix(path, `.`) {
		return nil, fmt.Errorf("Error with file '%s': paths cannot contain relative path components")
	}

	return strings.Split(path, `/`), nil
}

func LoadTorrent(name string) (*Torrent, error) {
	if file, err := os.Open(name); err == nil {
		hash := &Torrent{}

		if err := bencode.Unmarshal(file, hash); err == nil {
			return hash, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func CreateTorrent(name string, pieceLength int) (*Torrent, error) {
	if file, err := os.Open(name); err == nil {
		if stat, err := file.Stat(); err == nil {
			torrent := NewTorrent()

			if pieceLength > 0 {
				torrent.Info.PieceLength = pieceLength
			}

			buffer := make([]byte, torrent.Info.PieceLength)
			i := 0

			torrent.CreationEpoch = Epoch(time.Now().Unix())
			torrent.CreatedBy = DEFAULT_BF_CREATOR

			if err := torrent.AddFile(name, stat.Size()); err != nil {
				return nil, err
			}

			for {
				if n, err := file.Read(buffer); err == nil {
					sum := sha1.Sum(buffer[0:n])
					torrent.AddPiece(sum[:])
					// log.Debugf("Add Piece %d: %d bytes, hash: %x", i, n, sum)

					i += 1

					if n < torrent.Info.PieceLength {
						break
					}
				} else if err != io.EOF {
					return nil, err
				}
			}

			return torrent, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}
