package sdk

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"github.com/connctd/sdk-go/protocol"
	"github.com/golang/protobuf/proto"
	"log"
	"net"
	"net/url"
	"regexp"
	"sync"
)

var (
	PROTOCOL_VERSION uint64 = 1

	validNameRegexp = regexp.MustCompile("^[A-Za-z0-9]+$")
)

type OnDisconnectListener func()

type Client struct {
	conn          net.Conn
	host          string
	writer        *bufio.Writer
	receiveChan   chan protocol.ServerMessage
	things        []*Thing
	updateCounter uint64
	updateLock    *sync.Mutex
	OnDisconnect  OnDisconnectListener

	connected bool
}

func NewClient(url string) (*Client, error) {
	client := &Client{
		host:        url,
		receiveChan: make(chan protocol.ServerMessage, 10),
		things:      make([]*Thing, 0, 10),
		connected:   false,
		updateLock:  &sync.Mutex{},
	}
	return client, nil
}

func (c *Client) Connect(unitId, token string) error {
	var err error

	connUrl, err := url.Parse(c.host)
	if err != nil {
		return err
	}

	switch connUrl.Scheme {
	case "tcp":
		c.conn, err = net.Dial("tcp", connUrl.Host)
	case "ssl":
		tlsConf := &tls.Config{}
		c.conn, err = tls.Dial("tcp", connUrl.Host, tlsConf)
	}

	if err != nil {
		return err
	}
	c.writer = bufio.NewWriter(c.conn)

	hello := &protocol.ClientMessage_ClientHello{
		UnitId:          &unitId,
		Token:           &token,
		ProtocolVersion: &PROTOCOL_VERSION,
	}
	go c.read()
	go c.handleServerMessages()
	c.connected = true
	if err := c.send(&protocol.ClientMessage{Hello: hello}); err != nil {
		return err
	}
	return nil
}

func (c *Client) Disconnect() error {
	// TODO send disconnect message
	c.connected = false
	return c.conn.Close()
}

func (c *Client) IsConnected() bool {
	return c.connected
}

func (c *Client) validateThing(t *Thing) error {
	for _, thing := range c.things {
		if thing.Id == t.Id {
			return fmt.Errorf("The thing with the Id %s already exists", t.Id)
		}
	}
	for _, component := range t.Components {
		for _, property := range component.Properties {
			if !validNameRegexp.MatchString(property.Name) {
				return fmt.Errorf("%s is an invalid name for a property", property.Name)
			}
		}

		for _, action := range component.Actions {
			if !validNameRegexp.MatchString(action.Name) {
				return fmt.Errorf("%s is an invalid name for an action", action.Name)
			}
		}

		for _, capability := range component.Capabilities {
			for _, property := range capability.Properties {
				if !validNameRegexp.MatchString(property.Name) {
					return fmt.Errorf("%s is an invalid name for a property", property.Name)
				}
			}

			for _, action := range capability.Actions {
				if !validNameRegexp.MatchString(action.Name) {
					return fmt.Errorf("%s is an invalid name for an action", action.Name)
				}
			}
		}
	}
	// TODO Validate more stuff, but we have to decide what
	return nil
}

func (c *Client) abstract(t *Thing) error {
	// Validate the thing we are about to abstract
	if err := c.validateThing(t); err != nil {
		return err
	}
	// Set client to all Properties, so the property can
	// automatically send property changes
	for _, component := range t.Components {
		component.parent = t
		for _, property := range component.Properties {
			property.client = c
		}
		// Set the respective parents so properties can create their paths
		// for property update messages
		for _, capability := range component.Capabilities {
			for _, property := range capability.Properties {
				property.client = c
				property.parent = capability
			}
			capability.parent = component
		}
	}
	c.things = append(c.things, t)
	return nil
}

func (c *Client) Abstract(things ...*Thing) error {
	for _, thing := range things {
		if err := c.abstract(thing); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) RemoveThing(t *Thing) error {
	var thing *Thing
	var i int
	for i, thing = range c.things {
		if thing.Id == t.Id {
			break
		}
	}
	if i >= len(c.things) {
		return fmt.Errorf("Thing with Id %s not found", t.Id)
	}
	c.things = append(c.things[:i], c.things[i+1:]...)
	return nil
}

func (c *Client) PushThings() error {
	return c.sendThings()
}

func (c *Client) send(msg proto.Message) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	lenBytes := make([]byte, 4)
	lenLength := binary.PutUvarint(lenBytes, uint64(len(data)))
	_, err = c.writer.Write(lenBytes[:lenLength])
	if err != nil {
		return err
	}
	n, err := c.writer.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return fmt.Errorf("Written only %d bytes instead of %d", n, len(data))
	}
	return c.writer.Flush()
}

func (c *Client) read() {
	//reader := bufio.NewReader(c.conn)
	messageBuf := bytes.NewBuffer(make([]byte, 0, 4096))
	lengthBuf := bytes.NewBuffer(make([]byte, 0, 8))
	for {
		lengthBytes := make([]byte, 1, 1)
		readBytes, err := c.conn.Read(lengthBytes)
		if err != nil {
			log.Printf("Error reading amount of expected bytes from tcp connection: %v", err)
			break
		}
		if readBytes == 0 {
			continue
		}
		lengthBuf.Write(lengthBytes[:readBytes])
		expectedLength, err := binary.ReadUvarint(lengthBuf)
		if err != nil {
			continue
		}
		lengthBuf.Reset()
		var receivedBytesTotal uint64
		receivedBytesTotal = 0
		for receivedBytesTotal < expectedLength {
			remainingBytes := expectedLength - receivedBytesTotal
			dataBuf := make([]byte, remainingBytes, remainingBytes)
			readBytes, err = c.conn.Read(dataBuf)
			if err != nil {
				log.Printf("Error reading message from tcp connection: %v", err)
				break
			}
			// TODO check write to buffer
			messageBuf.Write(dataBuf[:readBytes])
			receivedBytesTotal = receivedBytesTotal + uint64(readBytes)
		}
		serverMessage := protocol.ServerMessage{}
		err = proto.Unmarshal(messageBuf.Bytes(), &serverMessage)
		if err != nil {
			log.Println("Error unmarshalling protobuf message")
			continue
		}
		messageBuf.Reset()
		c.receiveChan <- serverMessage
	}
	log.Printf("Disconnecting from server")
	c.conn.Close()
	c.connected = false
	if c.OnDisconnect != nil {
		c.OnDisconnect()
	}
}

func (c *Client) handleServerMessages() {
	for msg := range c.receiveChan {
		if msg.GetRequestThings() != nil {
			c.sendThings()
		}
		if msg.GetAction() != nil {
			c.handleAction(msg.GetAction())
		}
	}
}

func (c *Client) getThing(thingId string) *Thing {
	for _, thing := range c.things {
		if thingId == thing.Id {
			return thing
		}
	}
	return nil
}

func (c *Client) incrementupdateCounter() *uint64 {
	c.updateLock.Lock()
	c.updateCounter = c.updateCounter + 1
	c.updateLock.Unlock()
	val := c.updateCounter
	return &val
}

func (c *Client) sendThings() error {
	things := make([]*protocol.Thing, 0, len(c.things))
	for _, t := range c.things {
		things = append(things, t.Protocol())
	}

	response := &protocol.ClientMessage_RequestThingsResponse{
		UpdateLock: c.incrementupdateCounter(),
		Things:     things,
	}

	message := &protocol.ClientMessage{
		RequestThingsResponse: response,
	}
	return c.send(message)
}

func (c *Client) handleAction(msg *protocol.ServerMessage_Execute) {
	// TODO handle action
	if thing := c.getThing(msg.GetPath().GetThingId()); thing != nil {
		if component := thing.GetComponent(msg.GetPath().GetComponentId()); component != nil {
			if action := component.GetAction(msg.GetPath().GetAction()); action != nil {
				params := make([]string, 0, len(msg.GetParameters()))
				for _, param := range msg.GetParameters() {
					params = append(params, *param.Value)
				}
				status := protocol.ClientMessage_ExecutionResult_FAILURE
				var errorMsg string
				if err := action.Execute(*action, params); err == nil {
					status = protocol.ClientMessage_ExecutionResult_SUCCESS
				} else {
					errorMsg = fmt.Sprintf("%v", err)
				}
				result := protocol.ClientMessage_ExecutionResult{
					ErrorReason: &errorMsg,
					Result:      &status,
					Sequence:    msg.Sequence,
				}
				message := protocol.ClientMessage{
					ExecutionResult: &result,
				}
				c.send(&message)
			}
		}
	}

}
