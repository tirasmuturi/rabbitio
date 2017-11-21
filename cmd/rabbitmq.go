// Copyright © 2017 Meltwater
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"log"

	"github.com/streadway/amqp"
)

// RabbitMQ type for talking to RabbitMQ
type RabbitMQ struct {
	conn            *amqp.Connection
	channel         *amqp.Channel
	exchange        string
	contentType     string
	contentEncoding string
	queue           string
	tag             string
	routingKey      string
	prefetch        int
	consume         bool
	publish         bool
}

// Message contains the most basic about the message
type Message struct {
	Body       []byte
	RoutingKey string
	Headers    map[string]interface{}
}

// NewMessageFromAttrs will create a new message from a byte slice and attributes
func NewMessageFromAttrs(bytes []byte, attrs map[string]string) *Message {

	// add header information to the Message
	var headers = make(map[string]interface{})
	var key string
	for k, v := range attrs {
		switch k {
		// use the provided routing key to override tarball configuration
		case "amqp.routingKey":
			if routingKey != "" {
				key = routingKey
			} else {
				key = v
			}
		default:
			headers[k] = v
		}
	}

	// create a message
	m := &Message{
		Body:       bytes,
		RoutingKey: key,
		Headers:    headers,
	}

	return m
}

// NewRabbitMQ creates and sets up a RabbitOutput
func NewRabbitMQ(amqpURI, exchange, queue, routingKey, tag string, prefetch int, consume, publish bool) *RabbitMQ {
	conn, err := amqp.Dial(amqpURI)
	if err != nil {
		log.Fatalf("writer failed to connect to Rabbit: %s", err)
		return nil
	}

	go func() {
		log.Printf("writer closing: %s", <-conn.NotifyClose(make(chan *amqp.Error)))
		log.Printf("writer blocked by rabbit: %v", <-conn.NotifyBlocked(make(chan amqp.Blocking)))
	}()

	channel, err := conn.Channel()
	if err != nil {
		log.Fatalf("writer failed to get a channel from Rabbit: %s", err)
		return nil
	}

	if publish {
		if err = channel.ExchangeDeclarePassive(
			exchange, // name
			"topic",  // type
			true,     // durable
			false,    // auto-deleted
			false,    // internal
			false,    // noWait
			nil,      // arguments
		); err != nil {
			log.Fatalf("Exchange Declare: %s", err)
		}
	}

	if consume {

		q, err := channel.QueueDeclarePassive(
			queue, // name of the queue
			true,  // durable
			false, // delete when usused
			false, // exclusive
			false, // noWait
			nil,   // arguments
		)
		if err != nil {
			log.Fatalf("Queue Declare: %s", err)
		}
		if q.Messages == 0 {
			log.Fatalf("No messages in RabbitMQ Queue: %s", q.Name)
		}
		if err = channel.QueueBind(
			q.Name,   // name of the queue
			"#",      // bindingKey
			exchange, // sourceExchange
			false,    // noWait
			nil,      // arguments
		); err != nil {
			log.Fatalf("Queue Bind: %s", err)
		}
		log.Printf("Bind to Exchange: %q and Queue: %q, Messaging waiting: %d", exchange, queue, q.Messages)
	}

	r := &RabbitMQ{
		conn:            conn,
		channel:         channel,
		exchange:        exchange,
		contentType:     "application/json",
		contentEncoding: "UTF-8",
	}
	log.Print("RabbitMQ connected: ", amqpURI)

	return r
}

// Publish Takes stream of messages and publish them to rabbit
func (r *RabbitMQ) Publish(in chan Message) {
	for doc := range in {

		// var table amqp.Table = doc.Headers

		if err := r.channel.Publish(
			r.exchange,
			doc.RoutingKey,
			false, // mandatory
			false, // immediate
			amqp.Publishing{
				Headers:         doc.Headers,
				ContentType:     r.contentType,
				ContentEncoding: r.contentEncoding,
				Body:            doc.Body,
				DeliveryMode:    amqp.Persistent,
			},
		); err != nil {
			log.Fatalf("writer failed to write document to rabbit: %s", err)
		}
	}
}

// Consume outputs a stream of Message into a channel from rabbit
func (r *RabbitMQ) Consume(out chan Message) {

	// set up a channel consumer
	deliveries, err := r.channel.Consume(
		r.queue, // name
		r.tag,   // consumerTag,
		false,   // noAck
		false,   // exclusive
		false,   // noLocal
		false,   // noWait
		nil,     // arguments
	)
	if err != nil {
		log.Fatalf("rabbit consumer failed %s", err)
	}

	// process deliveries from the queue
	for d := range deliveries {
		// create a new Message for the rabbit message
		msg := Message{
			Body:       d.Body,
			RoutingKey: d.RoutingKey,
			Headers:    d.Headers,
		}
		// write Message to channel
		out <- msg
		// ack message
		r.channel.Ack(d.DeliveryTag, false)
	}

	log.Print("All messages consumed")

	// when deliveries are done, close
	close(out)
}
