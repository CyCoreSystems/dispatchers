package dispatchers

import (
	"log"
	"sync"

	"github.com/CyCoreSystems/dispatchers/v2/sets"
)

// An Exporter handles the export of the set of dispatcher sets.
type Exporter interface {
	Export([]*sets.State) error
}

// A Notifier handles the notification of interested parties of the change of dispatcher sets.
type Notifier interface {
	Notify([]*sets.State) error
}

// Controller manages the processing of dispatcher sets
type Controller struct{
	Exporter Exporter
	Notifier Notifier
	Logger *log.Logger

	sets []sets.DispatcherSet


	mu sync.RWMutex
}

// AddSet adds a DispatcherSet to the Controller
func (c *Controller) AddSet(set sets.DispatcherSet) {
	c.mu.Lock()

	c.sets = append(c.sets, set)

	c.mu.Unlock()
}

func (c *Controller) CurrentState() (currentState []*sets.State) {

	c.mu.RLock()
	for _, s := range c.sets {
		currentState = append(currentState, s.State())
	}
	c.mu.RUnlock()

	return currentState
}

// Export tells the Controller to export its current dispatcher sets
func (c *Controller) Export() error {
	if c.Exporter == nil {
		return nil
	}

	return c.Exporter.Export(c.CurrentState())
}

// Notify tells the Controller to send a notification to its notifier
func (c *Controller) Notify() error {
	if c.Notifier == nil {
		return nil
	}

	return c.Notifier.Notify(c.CurrentState())
}

// ChangeFunc provides a change handler for managing dispatcher set changes
func (c *Controller) ChangeFunc(state *sets.State) {
	currentState := c.CurrentState()

	if c.Exporter != nil {
		if err := c.Exporter.Export(currentState); err != nil {
			if c.Logger != nil {
				c.Logger.Println("failed to export current state:", err)
			}
		}
	}

	if c.Notifier != nil {
		if err := c.Notifier.Notify(currentState); err != nil {
			if c.Logger != nil {
				c.Logger.Println("failed to notify current state:", err)
			}
		}
	}
}
