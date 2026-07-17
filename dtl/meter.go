// Package dtl defines deterministic DTL execution contracts and resource
// metering. It depends only on state capabilities, never consensus or runtime.
package dtl

import (
	"errors"
	"fmt"

	"msc-chain/state"
)

type Version uint32

const (
	VersionV1 Version = 1
	VersionV2 Version = 2
)

type Limits struct {
	MaxReads       uint64
	MaxWrites      uint64
	MaxEvents      uint64
	MaxSteps       uint64
	MaxStorageByte uint64
	ReadFee        uint64
	WriteFee       uint64
	EventFee       uint64
	StepFee        uint64
	StorageByteFee uint64
}

type Usage struct {
	Reads        uint64
	Writes       uint64
	Events       uint64
	Steps        uint64
	StorageBytes uint64
	Fee          uint64
}

var ErrResourceLimit = errors.New("dtl: deterministic resource limit exceeded")

type Meter struct {
	limits Limits
	usage  Usage
}

func NewMeter(limits Limits) *Meter { return &Meter{limits: limits} }

func checkedAdd(current, delta, maximum uint64, resource string) (uint64, error) {
	if delta > ^uint64(0)-current || (maximum > 0 && current+delta > maximum) {
		return current, fmt.Errorf("%w: %s", ErrResourceLimit, resource)
	}
	return current + delta, nil
}

func (m *Meter) charge(counter *uint64, delta, maximum, unitFee uint64, resource string) error {
	if m == nil {
		return errors.New("dtl: nil meter")
	}
	next, err := checkedAdd(*counter, delta, maximum, resource)
	if err != nil {
		return err
	}
	if unitFee != 0 && delta > ^uint64(0)/unitFee {
		return fmt.Errorf("%w: fee", ErrResourceLimit)
	}
	feeDelta, err := checkedAdd(0, delta*unitFee, 0, "fee")
	if err != nil {
		return err
	}
	fee, err := checkedAdd(m.usage.Fee, feeDelta, 0, "fee")
	if err != nil {
		return err
	}
	*counter = next
	m.usage.Fee = fee
	return nil
}

func (m *Meter) Read() error {
	return m.charge(&m.usage.Reads, 1, m.limits.MaxReads, m.limits.ReadFee, "reads")
}
func (m *Meter) Write(storageBytes uint64) error {
	if m == nil {
		return errors.New("dtl: nil meter")
	}
	before := m.usage
	if err := m.charge(&m.usage.Writes, 1, m.limits.MaxWrites, m.limits.WriteFee, "writes"); err != nil {
		return err
	}
	if err := m.charge(&m.usage.StorageBytes, storageBytes, m.limits.MaxStorageByte, m.limits.StorageByteFee, "storage_bytes"); err != nil {
		m.usage = before
		return err
	}
	return nil
}
func (m *Meter) Event() error {
	return m.charge(&m.usage.Events, 1, m.limits.MaxEvents, m.limits.EventFee, "events")
}
func (m *Meter) Step(count uint64) error {
	return m.charge(&m.usage.Steps, count, m.limits.MaxSteps, m.limits.StepFee, "steps")
}
func (m *Meter) Usage() Usage {
	if m == nil {
		return Usage{}
	}
	return m.usage
}

type Context struct {
	Height uint64
	Reader state.Reader
	Writer state.Writer
	Meter  *Meter
}

type Executor[I, O any] interface {
	Version() Version
	Execute(Context, I) (O, error)
}

type Dispatcher[I, O any] struct {
	V1 Executor[I, O]
	V2 Executor[I, O]
}

func (d Dispatcher[I, O]) Execute(version Version, ctx Context, input I) (O, error) {
	var zero O
	var executor Executor[I, O]
	switch version {
	case VersionV1:
		executor = d.V1
	case VersionV2:
		executor = d.V2
	default:
		return zero, fmt.Errorf("dtl: unsupported executor version %d", version)
	}
	if executor == nil || executor.Version() != version {
		return zero, fmt.Errorf("dtl: executor version %d unavailable", version)
	}
	return executor.Execute(ctx, input)
}
