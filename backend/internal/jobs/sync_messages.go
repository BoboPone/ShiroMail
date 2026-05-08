package jobs

import (
	"context"
	"errors"
	"sync"

	"shiro-email/backend/internal/middleware"
	"shiro-email/backend/internal/modules/ingest"
	"shiro-email/backend/internal/modules/mailbox"
)

type MailboxSyncer interface {
	SyncMailbox(ctx context.Context, target mailbox.Mailbox) error
}

type InboundSpoolProcessor interface {
	ProcessNextSpool(ctx context.Context) error
}

func RunSyncMessagesJob(ctx context.Context, repo mailbox.Repository, syncService MailboxSyncer) error {
	if syncService == nil {
		return nil
	}

	items, err := repo.ListActive(ctx)
	if err != nil {
		return err
	}

	for _, item := range items {
		if err := syncService.SyncMailbox(ctx, item); err != nil {
			return err
		}
	}

	return nil
}

func RunInboundSpoolJob(ctx context.Context, processor InboundSpoolProcessor) error {
	if processor == nil {
		return nil
	}

	for {
		err := processor.ProcessNextSpool(ctx)
		switch {
		case err == nil:
			middleware.RecordInboundSpoolProcessed("completed")
			continue
		case errors.Is(err, ingest.ErrSpoolItemNotFound):
			return nil
		default:
			middleware.RecordInboundSpoolProcessed("failed")
			return err
		}
	}
}

func RunInboundSpoolJobConcurrent(ctx context.Context, processor InboundSpoolProcessor, workers int) error {
	if processor == nil {
		return nil
	}
	if workers <= 1 {
		return RunInboundSpoolJob(ctx, processor)
	}

	var (
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
	)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := RunInboundSpoolJob(ctx, processor); err != nil {
				errOnce.Do(func() {
					firstErr = err
				})
			}
		}()
	}

	wg.Wait()
	return firstErr
}
