package common

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
)

// ClientConfig Configuration used by the client
type ClientConfig struct {
	ID            string
	ServerAddress string
	LoopLapse     time.Duration
	LoopPeriod    time.Duration
}

// Client Entity that encapsulates how
type Client struct {
	config ClientConfig
	conn   net.Conn
}

// NewClient Initializes a new client receiving the configuration
// as a parameter
func NewClient(config ClientConfig) *Client {
	client := &Client{
		config: config,
	}
	return client
}

// CreateClientSocket Initializes client socket. In case of
// failure, error is printed in stdout/stderr and exit 1
// is returned
func (c *Client) createClientSocket() error {
	conn, err := net.Dial("tcp", c.config.ServerAddress)
	if err != nil {
		log.Fatalf(
			"action: connect | result: fail | client_id: %v | error: %v",
			c.config.ID,
			err,
		)
	}
	c.conn = conn
	return nil
}

// StartClientLoop Send messages to the client until some time threshold is met
func (c *Client) StartClientLoop() {
	// autoincremental msgID to identify every message sent
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGTERM)

	filename := "agency-" + c.config.ID + ".csv"
	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("Failed to open file: %s", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	total_sent := 0
	ack := 0
loop:
	// Send messages if the loopLapse threshold has not been surpassed
	for timeout := time.After(c.config.LoopLapse); ; {
		select {
		case <-timeout:
			log.Infof("action: timeout_detected | result: success | client_id: %v",
				c.config.ID,
			)
			//break loop

		case <-sigchan:
			log.Infof("CLIENTE RECIBIO SIGTERM")
			c.conn.Close()
			break loop

		default:
		}

		// Create the connection the server in every loop iteration. Send an
		c.createClientSocket()

		// SENDING
		//each bet has approximately 100 bytes
		batch_size := 8
		for i := 0; i < batch_size; i++ {
			last := 0
			if i == batch_size-1 {
				last = 1
			}
			message := create_message(c, reader, last)
			if message == "fin" {
				send_message(c, c.conn, "end|")
				log.Infof("finished reading: %v", c.config.ID)
				break loop
			}
			send_message(c, c.conn, message)
			total_sent++
		}

		//READING
		sv_answer := read_message(c, c.conn)
		answer := strings.Split(sv_answer, " ")
		if answer[0] == "err" {
			log.Infof("closing: %v", c.config.ID)
			break loop
		}

		amount, err := strconv.Atoi(answer[1])
		if err != nil {
			log.Fatalf("Failed to convert amount to int: %s", err)
		}
		ack += amount

		// Wait a time between sending one message and the next one
		time.Sleep(c.config.LoopPeriod)
	}
	log.Infof("TOTAL SENT %v", total_sent)
	//send "end"
	if ack < total_sent {
		sv_answer := read_message(c, c.conn)
		answer := strings.Split(sv_answer, " ")
		if answer[0] == "err" {
			log.Infof("closing: %v", c.config.ID)
		}
		//answer 1 to int
		amount, _ := strconv.Atoi(answer[1])
		ack += amount
	}
	// print ack and total sent
	fmt.Println("ACK: ", ack, " TOTAL SENT: ", total_sent)
	// Close the connection

	c.conn.Close()

	log.Infof("action: loop_finished | result: success | client_id: %v", c.config.ID)
}

func read_message(c *Client, conn net.Conn) string {
	bytes_read := 0
	reader := bufio.NewReader(conn)
	recv, err := reader.ReadString('\n')
	recv = recv[:len(recv)-1]
	if verify_recv_error(c, err) != nil {
		return err.Error()
	}

	bytes_read += len(recv)
	min_header_len := 2
	for bytes_read < min_header_len {
		read, err := reader.ReadString('\n')
		if verify_recv_error(c, err) != nil {
			return err.Error()
		}
		bytes_read += len(read)
	}

	header := ""
	for i := 0; i < len(recv); i++ {
		if recv[i] != '|' {
			header += string(recv[i])
		} else {
			break
		}
	}
	log.Infof("action: receive_message | result: success | client_id: %v | msg: %v",
		c.config.ID,
		recv,
	)
	// Return the message without the header
	//log.Infof("MSG without header: %v", recv[len(header):])
	return recv[len(header):]
}

func verify_recv_error(c *Client, err error) error {
	if err != nil {
		log.Errorf("action: receive_message | result: fail | client_id: %v | error: %v",
			c.config.ID,
			err,
		)
		return err
	}
	return nil
}

func send_message(c *Client, conn net.Conn, msg string) error {
	bytes_sent := 0
	for bytes_sent < len(msg) {
		bytes, err := fmt.Fprintf(
			conn,
			msg[bytes_sent:],
		)
		if err != nil {
			log.Errorf("action: send_message | result: fail | client_id: %v | error: %v",
				c.config.ID,
				err,
			)
			return err
		}
		bytes_sent += bytes
	}
	return nil
}

func read_csv_line(reader *bufio.Reader) string {
	line, err := reader.ReadString('\n')
	if errors.Is(err, io.EOF) {
		return ""
	}
	if err != nil {
		log.Fatalf("Failed to read line: %s", err)
	}
	return line
}

func create_message(c *Client, reader *bufio.Reader, last int) string {
	// HEADER 				BODY
	// len(msg) last msg(0/1) | msg
	msg := ""

	line := read_csv_line(reader)
	if line == "" {
		return "fin"
	}
	fields := strings.Split(line, ",")
	fields[4] = strings.TrimSuffix(fields[4], "\n")
	fields[4] = strings.TrimSuffix(fields[4], "\r")
	msg += fmt.Sprintf("|AGENCIA %v|NOMBRE %v|APELLIDO %v|DNI %v|NACIMIENTO %v|NUMERO %v|$", c.config.ID, fields[0], fields[1], fields[2], fields[3], fields[4]) //PONER CONSTANTES

	header := fmt.Sprintf("%v %v", last, len(msg))
	//header := fmt.Sprintf("%v ", len(msg_to_sv))
	msg = header + msg
	return msg
}
