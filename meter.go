package main

import (
	"sort"
	"time"
)

const meterBits = 6

type (
	MPoint struct {
		Timestamp int64
		Bytes     int64
	}

	Meter struct {
		Points [1 << meterBits]MPoint
		Index  int
	}
)

func MakeMeter(ts int64) (m Meter) {
	m.Init(ts)
	return m
}

func (m *Meter) Init(ts int64) {
	m.Index = 0
	m.Points[m.Index] = MPoint{Timestamp: ts}
}

func (m *Meter) Add(ts int64, b int) {
	const mask = 1<<meterBits - 1

	lbytes := m.bs(m.Index)

	m.Index++
	m.Points[m.Index&mask] = MPoint{
		Timestamp: ts,
		Bytes:     lbytes + int64(b),
	}
}

func (m *Meter) SpeedBPS() float64 {
	const window = time.Second

	l := m.Index - len(m.Points) + 1
	if l < 0 {
		l = 0
	}

	if m.dt(m.Index, l) > window {
		l = sort.Search(m.Index-l, func(i int) bool {
			return m.dt(m.Index, l+i) <= window
		})
	}

	//	tlog.Printw("meter.Speed", "low", l, "high", m.Index, "lp", m.at(l), "hp", m.at(m.Index))

	if l == m.Index {
		return float64(m.db(m.Index, m.Index-1)) / window.Seconds()
	}

	return float64(m.db(m.Index, l)) / m.dt(m.Index, l).Seconds()
}

func (m *Meter) dt(last, old int) time.Duration {
	return time.Duration(m.ts(last) - m.ts(old))
}

func (m *Meter) db(last, old int) int64 {
	return m.bs(last) - m.bs(old)
}

func (m *Meter) ts(i int) int64 {
	return m.at(i).Timestamp
}

func (m *Meter) bs(i int) int64 {
	return m.at(i).Bytes
}

func (m *Meter) at(i int) MPoint {
	const mask = 1<<meterBits - 1

	return m.Points[i&mask]
}
