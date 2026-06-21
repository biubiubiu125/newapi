package model

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func resetTicketTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(
		&Ticket{},
		&TicketMessage{},
		&TicketAttachment{},
		&TicketSequence{},
	))
	require.NoError(t, DB.Exec("DELETE FROM ticket_attachments").Error)
	require.NoError(t, DB.Exec("DELETE FROM ticket_messages").Error)
	require.NoError(t, DB.Exec("DELETE FROM tickets").Error)
	require.NoError(t, DB.Exec("DELETE FROM ticket_sequences").Error)
}

func TestNextTicketNumberReturnsUniqueSequentialNumbers(t *testing.T) {
	resetTicketTables(t)

	const count = 10
	var wg sync.WaitGroup
	results := make(chan int, count)
	errors := make(chan error, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, seq, err := NextTicketNumber(2026)
			if err != nil {
				errors <- err
				return
			}
			results <- seq
		}()
	}

	wg.Wait()
	close(results)
	close(errors)

	for err := range errors {
		require.NoError(t, err)
	}

	seen := map[int]bool{}
	for seq := range results {
		seen[seq] = true
	}
	require.Len(t, seen, count)
	for seq := TicketInitialSequence; seq < TicketInitialSequence+count; seq++ {
		require.True(t, seen[seq], "missing sequence %d", seq)
	}

	var ticketSeq TicketSequence
	require.NoError(t, DB.First(&ticketSeq, "year = ?", 2026).Error)
	require.Equal(t, TicketInitialSequence+count, ticketSeq.NextSeq)
}

func TestNextTicketNumberInitializesFromExistingTickets(t *testing.T) {
	resetTicketTables(t)

	existingSeq := TicketInitialSequence + 8
	require.NoError(t, DB.Create(&Ticket{
		Number:         fmt.Sprintf("RKAPI%d-%d", 2026, existingSeq),
		SequenceYear:   2026,
		SequenceNumber: existingSeq,
		UserId:         1,
		Username:       "tester",
		Title:          "existing ticket",
		Category:       TicketCategoryCustomer,
		Priority:       TicketPriorityNormal,
		Status:         TicketStatusPending,
	}).Error)

	number, seq, err := NextTicketNumber(2026)
	require.NoError(t, err)
	require.Equal(t, existingSeq+1, seq)
	require.Equal(t, fmt.Sprintf("RKAPI%d-%d", 2026, existingSeq+1), number)

	var ticketSeq TicketSequence
	require.NoError(t, DB.First(&ticketSeq, "year = ?", 2026).Error)
	require.Equal(t, existingSeq+2, ticketSeq.NextSeq)
}
