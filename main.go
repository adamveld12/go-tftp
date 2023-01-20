package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

var (
	Directory   = flag.String("directory", "", "directory to serve from")
	BindAddress = flag.String("address", "127.0.0.1", "address to bind to")
	Timeout     = flag.Duration("timeout", time.Second*5, "time until disconnect for session")
	Port        = flag.Int("port", 69, "port to listen on")
	Log         = flag.Bool("verbose", false, "verbose logging")
	sv          server
)

const (
	Opcode_RRQ   = Opcode(iota + 1)
	Opcode_WRQ   = Opcode(iota + 1)
	Opcode_DATA  = Opcode(iota + 1)
	Opcode_ACK   = Opcode(iota + 1)
	Opcode_ERROR = Opcode(iota + 1)

	ErrorCode_NotDefined      = ErrorCode(0)
	ErrorCode_FileNotFound    = ErrorCode(1)
	ErrorCode_AccessViolation = ErrorCode(2)
)

type (
	ErrorCode uint16
	Opcode    uint16
)

func (o ErrorCode) Message() []byte {
	var msg string

	switch o {
	case ErrorCode_NotDefined:
		msg = "Undefined error occurred"
		break
	case ErrorCode_FileNotFound:
		msg = "File Not Found"
		break
	case ErrorCode_AccessViolation:
		msg = "Access violation"
		break
	}
	//    0         Not defined, see error message (if any).
	//    1         File not found.
	//    2         Access violation.
	//    3         Disk full or allocation exceeded.
	//    4         Illegal TFTP operation.
	//    5         Unknown transfer ID.
	//    6         File already exists.
	//    7         No such user.
	return append([]byte(msg), 0x00)
}

func (o ErrorCode) Bytes() []byte {
	return append([]byte{0x00, byte(o & 0xFF)}, o.Message()...)
}

func (o Opcode) Bytes() []byte {
	return []byte{0x00, byte(o & 0xFF)}
}

func (o Opcode) String() string {
	switch o {
	case Opcode_RRQ:
		return "RRQ"
	case Opcode_WRQ:
		return "WRQ"
	case Opcode_DATA:
		return "DATA"
	case Opcode_ACK:
		return "ACK"
	case Opcode_ERROR:
		return "ERROR"
	}
	return "UNKNOWN"
}

type server struct {
	sessions map[string]*session
	*sync.Mutex
}

func (s *server) Get(addr net.Addr, l net.PacketConn) (*session, bool) {
	s.Lock()
	defer s.Unlock()
	key := addr.String()
	sess, ok := s.sessions[key]

	if !ok {
		ctx, canceler := context.WithCancel(context.Background())
		s.sessions[key] = &session{
			addr: addr,
			ctx:  ctx,
			w: func(dp DataPacket) {
				if _, err := l.WriteTo(dp, addr); err != nil {
					s.Close(key)
				}
			},
			msg: make(chan DataPacket),
		}

		go s.sessions[key].Run(canceler)
		sess = s.sessions[key]
	}

	return sess, ok
}

func (s *server) Close(key string) {
	s.Lock()
	defer s.Unlock()
	if session, ok := s.sessions[key]; ok {
		session.log("Closing session")
		delete(s.sessions, key)
		defer close(session.msg)
	}
}

type (
	PacketWriter func(DataPacket)
	session      struct {
		addr net.Addr
		ctx  context.Context
		w    PacketWriter
		msg  chan DataPacket
	}
)

func (s *session) Run(canceller context.CancelFunc) {
	tout := time.NewTimer(*Timeout)
	defer sv.Close(s.addr.String())
	defer canceller()
	defer tout.Stop()

	var initOpcode Opcode
	var sourceFile *os.File
	var blockSent uint64 = 1
	var transferFinished bool
	for {
		tout.Reset(*Timeout)
		select {
		case <-tout.C:
			s.log("timed out")
			return
		case dp := <-s.msg:
			if initOpcode == 0 && dp.Opcode() == Opcode_RRQ || dp.Opcode() == Opcode_WRQ {
				initOpcode = dp.Opcode()
				_, file, mode := dp.ParseRRQ()
				s.log("op: %s | file: %s | mode: %s", dp.Opcode(), file, mode)
				fp := filepath.Join(*Directory, file)
				f, err := os.Open(fp)
				if err != nil {
					if errors.Is(err, os.ErrNotExist) {
						s.log("File not found: %q", fp)
						s.w(NewErrorPacket(ErrorCode_FileNotFound))
						return
					}
					if errors.Is(err, os.ErrPermission) {
						s.log("Permission to read file denied: %q", fp)
						s.w(NewErrorPacket(ErrorCode_AccessViolation))
						return
					}

					s.w(NewErrorPacket(ErrorCode_NotDefined))
					return
				}

				sourceFile = f
				defer sourceFile.Close()
			}

			// doing file transfer to client
			if initOpcode == Opcode_RRQ {
				if dp.Opcode() == Opcode_ACK {
					_, blockID := dp.ParseAck()

					// TODO: will on retry sending the last block sent
					blockPtr := uint16(blockSent % 65535)
					if blockID == blockPtr {
						s.log("ACKed block %d", blockID)
						blockSent++
					} else if blockID > blockPtr {
						s.log("ACKed a block that we didnt send")
						s.w(NewErrorPacket(ErrorCode_AccessViolation))
						return
					} else {
						blockSent = blockSent - uint64(blockID) + 1
					}

					if transferFinished {
						s.log("transfer completed successfully")
						return
					}
				}

				data, err := readPacket(sourceFile, blockSent)
				if err != nil && !errors.Is(err, io.EOF) {
					s.w(NewErrorPacket(ErrorCode_AccessViolation))
					return
				}

				s.log("sending block %d - %d bytes", blockSent, len(data))
				s.w(NewDataPacket(uint16(blockSent%65535), data))

				if len(data) < 512 {
					transferFinished = true
					continue
				}
			} else if initOpcode == Opcode_WRQ {
				// Writing is not supported
				s.w(NewErrorPacket(ErrorCode_NotDefined))
				return
			}
		}
	}
}

func readPacket(f io.ReadSeeker, block uint64) ([]byte, error) {
	buf := [512]byte{}

	if _, err := f.Seek(int64((block-1)*512), io.SeekStart); err != nil {
		return nil, err
	}

	read, err := f.Read(buf[:])
	return buf[:read], err
}

func (s *session) log(msg string, args ...interface{}) {
	if *Log {
		log.Printf("[%s]: %s", s.addr, fmt.Sprintf(msg, args...))
	}
}

func main() {
	flag.Parse()
	address := fmt.Sprintf("%s:%d", *BindAddress, *Port)
	l, err := net.ListenPacket("udp", address)
	if err != nil {
		log.Fatalf("could not listen on %s: %+v", address, err)
	}
	defer l.Close()

	log.Printf("Listening @ %s", address)
	log.Printf("Serving files in %s", *Directory)

	sv = server{
		sessions: map[string]*session{},
		Mutex:    &sync.Mutex{},
	}

	sigs := make(chan os.Signal, 5)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)

	ctx, canceler := context.WithCancel(context.Background())

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:

				if err := l.SetReadDeadline(time.Now().Add(time.Second * 5)); err != nil {
					log.Panic("could not SetReadDeadline on listener:", err)
				}

				var (
					read int
					addr net.Addr
					err  error
					dp   = make(DataPacket, 516)
				)

				if read, addr, err = l.ReadFrom(dp[:]); err != nil || read == 0 {
					continue
				}

				if *Log {
					log.Printf("[%q -> server]: read %d bytes", addr.String(), read)
				}

				clientSession, ok := sv.Get(addr, l)
				if !ok {
					clientSession.log("creating new client session")
				}

				select {
				case clientSession.msg <- dp:
				}
			}
		}
	}()

	<-sigs
	canceler()
	log.Println("Shutting down...")
}

type DataPacket []byte

func NewDataPacketFromBytes(data ...[]byte) (DataPacket, error) {
	buf := &bytes.Buffer{}

	for _, packet := range data {
		buf.Write(packet)
		if buf.Len() > 516 {
			return nil, errors.New("too many bytes written")
		}
	}

	return buf.Bytes(), nil
}

func NewDataPacket(blockID uint16, data []byte) []byte {
	if len(data) > 512 {
		panic("TOO MUCH DATA")
	}

	opcode := Opcode_DATA.Bytes()
	dp := append([]byte{
		opcode[0], opcode[1],
		byte(blockID >> 8 & 0xFF), byte(blockID & 0xFF),
	}, data...)

	return dp
}

func NewErrorPacket(ec ErrorCode) DataPacket {
	dp, err := NewDataPacketFromBytes(Opcode_ERROR.Bytes(), ec.Bytes())
	if err != nil {
		log.Panicf("GENERATED BAD DATAPACKET: %v", err)
	}
	return dp
}

func (dp DataPacket) Opcode() Opcode {
	return Opcode(uint16(dp[0])<<8 | uint16(dp[1]))
}

func (dp DataPacket) ParseRRQ() (oc Opcode, file, mode string) {
	oc = Opcode(uint16(dp[0])<<8 | uint16(dp[1]))
	payload := bytes.Split(dp[2:], []byte{0x00})

	if len(payload) < 2 {
		panic("FILE PARSE FAILED")
	}

	file = string(payload[0])
	mode = string(payload[1])
	return
}

func (dp DataPacket) ParseAck() (oc Opcode, block uint16) {
	oc = Opcode(uint16(dp[0])<<8 | uint16(dp[1]))
	block = uint16(dp[2])<<8 | uint16(dp[3])
	return
}

func (dp DataPacket) Data() []byte {
	return dp[4:512]
}
