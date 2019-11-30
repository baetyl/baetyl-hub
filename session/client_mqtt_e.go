package session

import (
	"sync"
	"time"

	"github.com/baetyl/baetyl-broker/common"
	"github.com/baetyl/baetyl-go/utils/log"
)

type pm struct {
	e *common.Event
	p *common.Publish
	l time.Time // last send time
}

type publisher struct {
	d time.Duration
	m sync.Map
	c chan *pm
	n *common.Counter // Only used by mqtt client
}

func newPublisher(d time.Duration, c int) *publisher {
	return &publisher{
		d: d,
		c: make(chan *pm, c),
		n: common.NewCounter(),
	}
}

func (c *ClientMQTT) publish(e *common.Event) error {
	m := &pm{
		e: e,
		p: e.Packet(1),
		l: time.Now(),
	}
	m.p.ID = c.publisher.n.NextID()
	if o, ok := c.publisher.m.LoadOrStore(m.p.ID, m); ok {
		c.log.Error("packet id conflict, to acknowledge old one")
		o.(*pm).e.Done()
	}
	err := c.send(m.p, true)
	if err != nil {
		return err
	}
	select {
	case c.publisher.c <- m:
		return nil
	case <-c.Dying():
		return ErrClientClosed
	}
}

func (c *ClientMQTT) republish(m *pm) error {
	m.p.Dup = true
	m.l = time.Now()
	return c.send(m.p, true)
}

func (c *ClientMQTT) acknowledge(p *common.Puback) {
	m, ok := c.publisher.m.Load(p.ID)
	if !ok {
		c.log.Warn("puback not found", log.Int("id", int(p.ID)))
		return
	}
	c.publisher.m.Delete(p.ID)
	m.(*pm).e.Done()
}

func (c *ClientMQTT) publishing() (err error) {
	c.log.Info("client starts to publish messages")
	defer c.log.Info("client has stopped publishing messages")

	var e *common.Event
	qos0 := c.session.qos0.Chan()
	qos1 := c.session.qos1.Chan()
	for {
		select {
		case e = <-qos0:
			c.send(e.Packet(0), false)
		case e = <-qos1:
			if err := c.publish(e); err != nil {
				return err
			}
		case <-c.Dying():
			return nil
		}
	}
}

func (c *ClientMQTT) republishing() error {
	c.log.Info("client starts to republish messages", log.Duration("interval", c.publisher.d))
	defer c.log.Info("client has stopped republishing messages")

	var m *pm
	timer := time.NewTimer(c.publisher.d)
	defer timer.Stop()
	for {
		select {
		case m = <-c.publisher.c:
			for timer.Reset(c.publisher.d - time.Now().Sub(m.l)); m.e.Wait(timer.C, c.Dying()) == common.ErrAcknowledgeTimedOut; timer.Reset(c.publisher.d) {
				if err := c.republish(m); err != nil {
					return err
				}
			}
		case <-c.Dying():
			return nil
		}
	}
}

func (c *ClientMQTT) sendConnack(code common.ConnackCode, exists bool) error {
	ack := &common.Connack{
		SessionPresent: exists,
		ReturnCode:     code,
	}
	return c.send(ack, false)
}

func (c *ClientMQTT) send(pkt common.Packet, async bool) error {
	// TODO: remove lock
	c.Lock()
	err := c.connection.Send(pkt, async)
	c.Unlock()
	if err != nil {
		c.log.Error("failed to send packet", log.Error(err))
		c.die(err)
		return err
	}
	if ent := c.log.Check(log.DebugLevel, "client sent a packet"); ent != nil {
		ent.Write(log.String("packet", pkt.String()))
	}
	return nil
}
