package powsrv

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/muxxer/powsrv/logs"

	"github.com/iotaledger/giota"
	"github.com/sigurn/crc8"
)

// PowClient is the client that connects to the powSrv
type PowClient struct {
	Network        string   // Network of the powSrv ("unix", "tcp")
	Address        string   // Address of the powSrv ("Unix socket", "IP:port"
	WriteTimeOutMs int64    // Timeout in ms to write to the Unix socket
	ReadTimeOutMs  int      // Timeout in ms to read the Unix socket
	Connection     net.Conn // Connection to the powSrv
	RequestId      byte
	RequestIdLock  sync.Mutex
}

var responses map[byte]*IpcFrameV1
var responsesLock = &sync.Mutex{}

func (p *PowClient) Init() {
	var err error
	responses = make(map[byte]*IpcFrameV1)
	p.Connection, err = net.Dial(p.Network, p.Address)
	if err != nil {
		logs.Log.Fatal(err.Error())
	}
	go p.receive()
}

func (p *PowClient) Close() {
	p.Connection.Close()
}

func (p *PowClient) receive() {
	frameState := FrameStateSearchEnq
	frameLength := 0
	var frameData []byte

	if p.Connection == nil {
		logs.Log.Error("Connection not established")
	}

	for {
		buf := make([]byte, 3072) // ((8019 is the TransactionTrinarySize) / 3) + Overhead) => 3072
		bufLength, err := p.Connection.Read(buf)
		if err != nil {
			continue
		}

		bufferIdx := -1
		for {
			bufferIdx++

			if bufLength > bufferIdx {
				switch frameState {

				case FrameStateSearchEnq:
					if buf[bufferIdx] == 0x05 {
						// Init variables for new message
						frameLength = -1
						frameData = nil
						frameState = FrameStateSearchVersion
					}

				case FrameStateSearchVersion:
					if buf[bufferIdx] == 0x01 {
						frameState = FrameStateSearchLength
					} else {
						frameState = FrameStateSearchEnq
					}

				case FrameStateSearchLength:
					if frameLength == -1 {
						// Receive first byte
						frameLength = int(buf[bufferIdx]) << 8
					} else {
						// Receive second byte and go on
						frameLength |= int(buf[bufferIdx])
						frameState = FrameStateSearchData
					}

				case FrameStateSearchData:
					missingByteCount := frameLength - len(frameData)
					if (bufLength - bufferIdx) >= missingByteCount {
						// Frame completely received
						frameData = append(frameData, buf[bufferIdx:(bufferIdx+missingByteCount)]...)
						bufferIdx += missingByteCount - 1
						frameState = FrameStateSearchCRC
					} else {
						// Frame not completed in this read => Copy the remaining bytes
						frameData = append(frameData, buf[bufferIdx:bufLength]...)
						bufferIdx = bufLength
					}

				case FrameStateSearchCRC:
					crc := crc8.Checksum(frameData, crc8Table)
					if buf[bufferIdx] != crc {
						logs.Log.Debugf("Wrong Checksum! CRC: %X, Expected: %X", crc, buf[bufferIdx])
						frameState = FrameStateSearchEnq
						break
					}

					frame, err := BytesToIpcFrameV1(frameData)
					if err != nil {
						logs.Log.Debug("Can't convert bytes to IpcFrame")
						frameState = FrameStateSearchEnq
						break
					}

					responsesLock.Lock()
					responses[frame.ReqID] = frame
					responsesLock.Unlock()
					frameState = FrameStateSearchEnq
				}
			} else {
				// Received Buffer completely handled, break the loop to receive the next message
				break
			}
		}
	}
}

// sendToServer sends an IpcMessage struct to the powSrv
// It returns the response bytes or an error
func (p *PowClient) sendToServer(requestMsg *IpcMessage) (Error error) {
	request, err := requestMsg.ToBytes()
	if err != nil {
		return err
	}

	if p.Connection == nil {
		return errors.New("Connection not established")
	}

	if p.WriteTimeOutMs != 0 {
		err = p.Connection.SetWriteDeadline(time.Now().Add(time.Millisecond * time.Duration(p.WriteTimeOutMs)))
		if err != nil {
			return err
		}
	}

	/*
		if p.ReadTimeOutMs != 0 {
			err = c.SetReadDeadline(time.Now().Add(time.Millisecond * time.Duration(p.ReadTimeOutMs)))
			if err != nil {
				return err
			}
		}
	*/

	_, err = p.Connection.Write(request)

	return err
}

// sendIpcFrameV1ToServer creates an IpcFrameV1 and calls sendToServer
// The answer of the server is evaluated and returned to the caller
func (p *PowClient) sendIpcFrameV1ToServer(command byte, data []byte) (response []byte, Error error) {
	p.RequestIdLock.Lock()
	p.RequestId++
	reqID := p.RequestId
	p.RequestIdLock.Unlock()

	var frame *IpcFrameV1
	var ok bool

	requestMsg, err := NewIpcMessageV1(reqID, command, data)
	if err != nil {
		return nil, err
	}

	err = p.sendToServer(requestMsg)
	if err != nil {
		return nil, err
	}

	ts := time.Now()
	td := time.Duration(p.ReadTimeOutMs) * time.Millisecond

	for {
		if time.Since(ts) > td {
			return nil, errors.New("Receive timeout")
		}

		responsesLock.Lock()
		frame, ok = responses[reqID]
		if ok {
			delete(responses, reqID)
		}
		responsesLock.Unlock()
		if ok {
			break
		}
	}

	switch frame.Command {

	case IpcCmdResponse:
		return frame.Data, nil

	case IpcCmdError:
		return nil, fmt.Errorf(string(frame.Data))

	default:
		//
		// IpcCmdNotification, IpcCmdGetServerVersion, IpcCmdGetPowType, IpcCmdGetPowVersion, IpcCmdPowFunc
		return nil, fmt.Errorf("Unknown command! Cmd: %X", frame.Command)
	}
}

// GetPowInfo returns information about the powSrv version, POW hardware type, and POW hardware version
func (p *PowClient) GetPowInfo() (ServerVersion string, PowType string, PowVersion string, Error error) {
	serverVersion, err := p.sendIpcFrameV1ToServer(IpcCmdGetServerVersion, nil)
	if err != nil {
		return "", "", "", err
	}

	powType, err := p.sendIpcFrameV1ToServer(IpcCmdGetPowType, nil)
	if err != nil {
		return "", "", "", err
	}

	powVersion, err := p.sendIpcFrameV1ToServer(IpcCmdGetPowVersion, nil)
	if err != nil {
		return "", "", "", err
	}

	return string(serverVersion), string(powType), string(powVersion), nil
}

// PowFunc does the POW
func (p *PowClient) PowFunc(trytes giota.Trytes, minWeightMagnitude int) (result giota.Trytes, Error error) {
	if (minWeightMagnitude < 0) || (minWeightMagnitude > 243) {
		return "", fmt.Errorf("minWeightMagnitude out of range [0-243]: %v", minWeightMagnitude)
	}

	data := []byte{byte(minWeightMagnitude)}
	data = append(data, []byte(string(trytes))...)

	response, err := p.sendIpcFrameV1ToServer(IpcCmdPowFunc, data)
	if err != nil {
		return "", err
	}

	result, err = giota.ToTrytes(string(response))
	if err != nil {
		return "", err
	}

	return result, err
}
