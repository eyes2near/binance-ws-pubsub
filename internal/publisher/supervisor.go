package publisher

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

func RunSupervisor(ctx context.Context, cfg SupervisorConfig) error {
	if !cfg.PublishOrderbook && !cfg.PublishTrades {
		return errors.New("nothing to do: both -orderbook and -trades are disabled")
	}
	if cfg.BrokerURL == "" {
		return errors.New("missing broker url")
	}
	if cfg.Market.Symbol == "" {
		return errors.New("missing resolved symbol")
	}

	backoff := 500 * time.Millisecond
	maxBackoff := 30 * time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		bc, err := DialBroker(ctx, cfg.BrokerURL)
		if err != nil {
			log.Printf("[publisher] dial broker failed: %v", err)
			sleep(ctx, backoff)
			backoff = incBackoff(backoff, maxBackoff)
			continue
		}
		log.Printf("[publisher] connected to broker: %s", cfg.BrokerURL)

		runCtx, cancel := context.WithCancel(ctx)

		brokerErrCh := bc.StartReadPump(runCtx)
		workerErrCh := make(chan error, 2)

		if cfg.PublishOrderbook {
			w := OrderbookWorker{Market: cfg.Market, Cfg: cfg.Orderbook}
			go func() {
				workerErrCh <- fmt.Errorf("orderbook worker: %w", w.Run(runCtx, bc))
			}()
		}

		if cfg.PublishTrades {
			w := TradesWorker{Market: cfg.Market, Cfg: cfg.Trades}
			go func() {
				workerErrCh <- fmt.Errorf("trades worker: %w", w.Run(runCtx, bc))
			}()
		}

		var terminalErr error
		select {
		case <-ctx.Done():
			terminalErr = ctx.Err()

		case err := <-brokerErrCh:
			terminalErr = fmt.Errorf("broker read loop ended: %w", err)

		case err := <-workerErrCh:
			terminalErr = err
		}

		cancel()
		_ = bc.Close()

		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Printf("[publisher] restarting after error: %v", terminalErr)
		sleep(ctx, backoff)
		backoff = incBackoff(backoff, maxBackoff)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

func incBackoff(cur, max time.Duration) time.Duration {
	n := cur * 2
	if n > max {
		return max
	}
	return n
}
