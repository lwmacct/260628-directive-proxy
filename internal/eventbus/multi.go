package eventbus

import (
	"context"
	"errors"
)

type MultiPublisher struct {
	publishers []Publisher
}

func NewMultiPublisher(publishers ...Publisher) Publisher {
	out := make([]Publisher, 0, len(publishers))
	for _, publisher := range publishers {
		if publisher != nil {
			out = append(out, publisher)
		}
	}
	if len(out) == 0 {
		return NopPublisher{}
	}
	if len(out) == 1 {
		return out[0]
	}
	return &MultiPublisher{publishers: out}
}

func (p *MultiPublisher) Publish(ctx context.Context, event Event) error {
	if p == nil {
		return nil
	}
	var errs []error
	for _, publisher := range p.publishers {
		if err := publisher.Publish(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (p *MultiPublisher) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	var errs []error
	for _, publisher := range p.publishers {
		if err := publisher.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
