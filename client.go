package powsrv

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/iotaledger/giota"
	"github.com/sigurn/crc8"
)

// PowClient is the client that connects to the powSrv
type PowClient struct {
	PowSrvPath     string // Path to the powSrv Unix socket
	WriteTimeOutMs int64  // Timeout in ms to write to the Unix socket
	ReadTimeOutMs  int    // Timeout in ms to read the Unix socket
}

var reqID byte

func receive(c net.Conn, timeoutMs int) (response []byte, Error error) {
	frameState := FrameStateSearchEnq
	frameLength := 0
	var frameData []byte

	ts := time.Now()
	td := time.Duration(timeoutMs) * time.Millisecond

	for {
		if time.Since(ts) > td {
			return nil, errors.New("Receive timeout")
		}

		buf := make([]byte, 3072) // ((8019 is the TransactionTrinarySize) / 3) + Overhead) => 3072
		bufLength, err := c.Read(buf)
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
						return nil, fmt.Errorf("Wrong Checksum! CRC: %X, Expected: %X", crc, buf[bufferIdx])
					}

					return frameData, nil

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
func (p PowClient) sendToServer(requestMsg *IpcMessage) (response []byte, Error error) {
	request, err := requestMsg.ToBytes()
	if err != nil {
		return nil, err
	}

	c, err := net.Dial("unix", p.PowSrvPath)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	if p.WriteTimeOutMs != 0 {
		err = c.SetWriteDeadline(time.Now().Add(time.Millisecond * time.Duration(p.WriteTimeOutMs)))
		if err != nil {
			return nil, err
		}
	}

	if p.ReadTimeOutMs != 0 {
		err = c.SetReadDeadline(time.Now().Add(time.Millisecond * time.Duration(p.ReadTimeOutMs)))
		if err != nil {
			return nil, err
		}
	}

	_, err = c.Write(request)
	if err != nil {
		return nil, err
	}

	response, err = receive(c, p.ReadTimeOutMs)
	return response, err
}

// sendIpcFrameV1ToServer creates an IpcFrameV1 and calls sendToServer
// The answer of the server is evaluated and returned to the caller
func (p PowClient) sendIpcFrameV1ToServer(command byte, data []byte) (response []byte, Error error) {
	reqID++

	requestMsg, err := NewIpcMessageV1(reqID, command, data)
	if err != nil {
		return nil, err
	}

	resp, err := p.sendToServer(requestMsg)
	if err != nil {
		return nil, err
	}

	frame, err := BytesToIpcFrameV1(resp)
	if err != nil {
		return nil, err
	}

	if frame.ReqID != reqID {
		return nil, fmt.Errorf("Wrong ReqID! ReqID: %X, Expected: %X", frame.ReqID, reqID)
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

// InitPow initializes the POW hardware
func (p PowClient) InitPow() error {
	_, err := p.sendIpcFrameV1ToServer(IpcCmdInitPOW, nil)
	if err != nil {
		return err
	}

	return nil
}

// GetPowInfo returns information about the powSrv version, POW hardware type, and POW hardware version
func (p PowClient) GetPowInfo() (ServerVersion string, PowType string, PowVersion string, Error error) {
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
func (p PowClient) PowFunc(trytes giota.Trytes, minWeightMagnitude int) (result giota.Trytes, Error error) {
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
