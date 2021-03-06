package event

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ovh/cds/engine/api/cache"
	"github.com/ovh/cds/sdk"
	"github.com/ovh/cds/sdk/log"
	"github.com/ovh/cds/sdk/namesgenerator"
)

var hostname, cdsname string
var kafkaBroker Broker
var brokers []Broker
var subscribers []chan<- sdk.Event

func init() {
	subscribers = make([]chan<- sdk.Event, 0)
}

// Broker event typed
type Broker interface {
	initialize(options interface{}) (Broker, error)
	sendEvent(event *sdk.Event) error
	status() string
	close()
}

func getBroker(t string, option interface{}) (Broker, error) {
	switch t {
	case "kafka":
		k := &KafkaClient{}
		return k.initialize(option)
	}
	return nil, fmt.Errorf("Invalid Broker Type %s", t)
}

// Initialize initializes event system
func Initialize(k KafkaConfig, cache cache.Store) error {
	store = cache
	var err error
	hostname, err = os.Hostname()
	if err != nil {
		hostname = fmt.Sprintf("Error while getting Hostname: %s", err.Error())
	}
	// generates an API name. api_foo_bar, only 3 first letters to have a readable status
	cdsname = "api_"
	for _, v := range strings.Split(namesgenerator.GetRandomNameCDS(0), "_") {
		if len(v) > 3 {
			cdsname += v[:3]
		} else {
			cdsname += v
		}
	}

	brokers = []Broker{}
	if k.Enabled {
		var errk error
		kafkaBroker, errk = getBroker("kafka", k)
		if errk != nil {
			return errk
		}
		brokers = append(brokers, kafkaBroker)
	}
	return nil
}

// Subscribe to CDS events
func Subscribe(ch chan<- sdk.Event) {
	subscribers = append(subscribers, ch)
}

// DequeueEvent runs in a goroutine and dequeue event from cache
func DequeueEvent(c context.Context) {
	for {
		e := sdk.Event{}
		store.DequeueWithContext(c, "events", &e)
		if err := c.Err(); err != nil {
			log.Error("Exiting event.DequeueEvent : %v", err)
			return
		}

		for _, s := range subscribers {
			s <- e
		}

		// Send into external brokers
		for _, b := range brokers {
			if err := b.sendEvent(&e); err != nil {
				log.Warning("Error while sending message [%s: %s/%s/%s/%s/%s]: %s", e.EventType, e.ProjectKey, e.WorkflowName, e.ApplicationName, e.PipelineName, e.EnvironmentName, err)
			}
		}
	}
}

// GetHostname returns Hostname of this cds instance
func GetHostname() string {
	return hostname
}

// GetCDSName returns cdsname of this cds instance
func GetCDSName() string {
	return cdsname
}

// Close closes event system
func Close() {
	for _, b := range brokers {
		b.close()
	}
}

// Status returns Event status
func Status() sdk.MonitoringStatusLine {
	var o string
	var isAlert bool
	for _, b := range brokers {
		s := b.status()
		if !strings.Contains(s, "OK") {
			isAlert = true
		}
		o += s + " "
	}

	if o == "" {
		o = "⚠ "
	}
	status := sdk.MonitoringStatusOK
	if isAlert {
		status = sdk.MonitoringStatusAlert
	}

	return sdk.MonitoringStatusLine{Component: "Event Broker", Value: o, Status: status}
}
