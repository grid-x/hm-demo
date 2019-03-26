package main

import (
	"encoding/binary"
	"fmt"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"periph.io/x/periph/conn/physic"
	"periph.io/x/periph/conn/spi"
	"periph.io/x/periph/conn/spi/spireg"
	"periph.io/x/periph/host"
)

func main() {
	var (
		logger = log.New()

		mqttBroker = flag.String("mqtt.broker", "tcp://127.0.0.1:1883", "Broker to connect to")

		spiDev      = flag.String("spi.dev", "SPI0.0", "SPI port")
		spiMode     = flag.Int("spi.mode", 0x0, "Communication Mode")
		spiBits     = flag.Int("spi.bits", 8, "Number of bits per word")
		spiMaxSpeed = flag.Int("spi.max-speed", 2800000, "Maximum rated speed by the device's spec in µHz")
	)
	flag.Parse()

	mqttLogFields := log.Fields{
		"mqtt.broker": *mqttBroker,
	}
	opts := mqtt.NewClientOptions()
	opts.AddBroker(*mqttBroker)
	client := mqtt.NewClient(opts)
	// Establish a mqtt connection.
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		logger.WithFields(mqttLogFields).Fatalf("mqtt.Connect: %+v", token.Error())
	}
	logger.WithFields(mqttLogFields).Infof("mqtt.Connect: connected")

	// Close mqtt connection when program exists.
	defer client.Disconnect(250)

	spiLogFields := log.Fields{
		"spi.dev":       *spiDev,
		"spi.mode":      spi.Mode(*spiMode).String(),
		"spi.bits":      *spiBits,
		"spi.max-speed": physic.Frequency(*spiMaxSpeed),
	}

	// Initialize periph.
	if _, err := host.Init(); err != nil {
		logger.WithFields(spiLogFields).Fatalf("host.Init: %+v", err)
	}

	// Open SPI port.
	port, err := spireg.Open(*spiDev)
	if err != nil {
		logger.WithFields(spiLogFields).Fatalf("spireg.Open: %+v", err)
	}

	/*
		// For testing only
		port := spitest.Record{
			Port: &spitest.Playback{
				Playback: conntest.Playback{
					Ops: []conntest.IO{
						{W: nil, R: []byte{'a', 'b', 'c', 0, 0, 4, 0, 0}},
						{W: nil, R: []byte{'a', 'b', 'd', 0, 150, 142, 202, 1}},
					},
					DontPanic: true,
				},
			},
		}
	*/

	// Close port when program exists.
	defer port.Close()

	// Connect with device over SPI protocol.
	conn, err := port.Connect(physic.Frequency(*spiMaxSpeed), spi.Mode(*spiMode), *spiBits)
	if err != nil {
		logger.WithFields(spiLogFields).Fatalf("spi.Connect: %+v", err)
	}

	// Messages is a channel.
	messages := make(chan Message)

	go func() {
		buf := make([]byte, 8) //  8 Bytes are read at once.
		for {
			err := conn.Tx(nil, buf[:])
			if err != nil {
				logger.WithFields(spiLogFields).Errorf("spi.Tx: %+v", err)
				close(messages)
				return
			}
			msg, err := NewMessage(buf[:])
			if err != nil {
				logger.WithFields(spiLogFields).Fatalf("NewMessage: %+v", err)
				continue
			}
			messages <- msg // Put parsed message into channel.
		}
	}()

	for msg := range messages { // Read from channel.
		logger.WithFields(mqttLogFields).WithField("mqtt.topic", msg.Topic).
			Infof("Send message: %s: %+v", msg.Topic, msg.Bytes())

		token := client.Publish(msg.Topic, 0x0, false, msg.Bytes())
		token.Wait()
		if token.Error() != nil {
			logger.WithFields(mqttLogFields).WithField("mqtt.topic", msg.Topic).
				Errorf("mqtt.Publish(%s, %q): %+v", msg.Topic, msg.Bytes(), token.Error())
		}
	}
}

// Message represents an mqtt message.
type Message struct {
	Topic string
	//  TODO(s.rau): Adjust the number of fields.
	Value int
}

// NewMessage converts SPI bytes into a message struct.
func NewMessage(d []byte) (Message, error) {
	if len(d) != 8 {
		return Message{}, fmt.Errorf("NewMessage: mismatch number of bytes %d != 8", len(d))
	}

	return Message{
		Topic: string(d[0:3]),
		// TODO (s.rau): convert the SPI read to actual values.
		Value: int(binary.LittleEndian.Uint32(d[4:])),
	}, nil
}

// Bytes returns a byte representation of the value(s).
func (m Message) Bytes() []byte {
	// For JSON output, remove comment
	//d, _ := json.Marshal(m)
	//return d
	return []byte(fmt.Sprintf("%d", m.Value))
}
