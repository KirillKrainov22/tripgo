package positions

import "time"

type Ticker interface {
	Chan() <-chan time.Time
	Stop()
}

type realTicker struct {
	ticker *time.Ticker
}

func NewTicker(period time.Duration) Ticker {
	return realTicker{ticker: time.NewTicker(period)}
}

func (t realTicker) Chan() <-chan time.Time {
	return t.ticker.C
}

func (t realTicker) Stop() {
	t.ticker.Stop()
}
