package main

import (
	"encoding/binary"
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"periph.io/x/periph/conn/physic"
	"periph.io/x/periph/conn/spi"
	"periph.io/x/periph/conn/spi/spireg"
	"periph.io/x/periph/host"
	crc "github.com/howeyc/crc16"
)


func main() {
	var (
		logger = log.New()

		mqttBroker = flag.String("mqtt.broker", "tcp://192.168.0.107:1883", "Broker to connect to")

		spiDev      = flag.String("spi.dev", "SPI1.0", "SPI port")
		spiMode     = flag.Int("spi.mode", 0x0, "Communication Mode")
		spiBits     = flag.Int("spi.bits", 8, "Number of bits per word")
		spiMaxSpeed = flag.Int64("spi.max-speed", 40000000000, "Maximum rated speed by the device's spec in µHz")
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

	//execLogFields := logFileds{
	//	"excec" : *..
	//}
/*
	cmd := exec.Command("gpio", "write", "1", " 1")

	execLogFields := log.Fields{
              "excec" : *cmd,
        }

	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()

	if err != nil {
		logger.WithFields(execLogFields).Fatalf("cant change reset pin", err)
	}else{
		// give uC time to boot (need around 500 - 600ms)
		time.Sleep(750 * time.Millisecond)
		logger.WithFields(execLogFields).Infof("Changed /Reset Pin to 1")
	}
*/

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


	// For testing only
/*	port := spitest.Record{
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
		buf := make([]byte, 11) //  11 Bytes are read at once.
		for {
			err := conn.Tx(nil, buf[:])
			if err != nil {
				logger.WithFields(spiLogFields).Errorf("spi.Tx: %+v", err)
				close(messages)
				return

			}
			crcValue := crc.ChecksumIBM(buf[1:])

			//logger.WithFields(spiLogFields).Infof("CRC: %X",crcValue)
			//logger.WithFields(spiLogFields).Infof("Bytes: %#X", buf[:])

			if crcValue == 0xFFFF{
				msg, err := NewMessage(buf[:])
				if err != nil {
					logger.WithFields(spiLogFields).Fatalf("NewMessage: %+v", err)
					continue
				}

				//logger.WithFields(spiLogFields).Infof("Bytes: %q", buf[:])
				messages <- msg // Put parsed message into channel.
			}else{
				logger.WithFields(spiLogFields).Errorf("CRC ERROR: %X", crcValue)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}()

	for msg := range messages { // Read from channel.
		//logger.WithFields(mqttLogFields).WithField("mqtt.topic", msg.Topic).Infof("Send message: %s: %+v", msg.Topic, msg.Bytes()
		if msg.Freq == 32776  {
			msg.Freq = 0
		}
		if msg.StatusFlags == 0 {
			msg.PlcID = 0
		}
		SendMqtt("sensors/gridBox/rpm", msg.Freq, client, logger, mqttLogFields)
		SendMqtt("sensors/gridBox/plcId", msg.PlcID, client, logger, mqttLogFields)
		SendMqtt("sensors/gridBox/count", msg.CounterValue, client,  logger, mqttLogFields)
		SendMqtt("sensors/gridBox/status", msg.StatusFlags, client, logger, mqttLogFields)
		time.Sleep(250 * time.Millisecond)
	}
}

// Message represents an mqtt message.
type Message struct {
	StatusFlags	int;
	ErrorCode	int;
	PlcID		int;
	Freq		int;
	CounterValue	int;
	Crc16		int;
}


// NewMessage converts SPI bytes into a message struct.
func NewMessage(d []byte) (Message, error) {
	if len(d) != 11 {
		return Message{}, fmt.Errorf("NewMessage: mismatch number of bytes %d != 11", len(d))
	}

	return Message{
		//d[0] empty Byte, should be 0xAA
		StatusFlags	: int(d[1]),
		ErrorCode	: int(d[2]),
		PlcID		: int(binary.LittleEndian.Uint16(d[3:5])),
		Freq		: int(binary.LittleEndian.Uint16(d[5:7])),
		CounterValue    : int(binary.LittleEndian.Uint16(d[7:9])),
//		Crc16		: int(binary.LittleEndian.Uint16(d[9:11])),
	}, nil

}

func SendMqtt(topic string, value int, client mqtt.Client,logger *log.Logger,fields log.Fields) error {
	token := client.Publish(topic, 0x0, false, []byte(fmt.Sprintf("%d", value)))
        token.Wait()

//	logger.WithFields(fields).WithField("mqtt.topic", topic).Infof("Send message: %s: %+v", topic,value)

	if token.Error() != nil {
	        logger.WithFields(fields).WithField("mqtt.topic", topic).Errorf("mqtt.Publish(%s, %x: %+v", topic, value, token.Error())
        }
	return token.Error()
}

