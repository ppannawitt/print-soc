package catalog

import "testing"

func TestEmbeddedCatalogCountsAndQueues(t *testing.T) {
	printers, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(printers) != 58 {
		t.Fatalf("catalog has %d printers, want 58", len(printers))
	}
	queueCount, publicCount := 0, 0
	for _, printer := range printers {
		queueCount += len(printer.Queues)
		if IsStudentEligible(printer) {
			publicCount++
		}
	}
	if queueCount != 134 {
		t.Fatalf("catalog has %d queues, want 134", queueCount)
	}
	if publicCount != 8 {
		t.Fatalf("catalog has %d student printers, want 8", publicCount)
	}
}

func TestValidateQueueEnforcesPrinterAndStudentAccess(t *testing.T) {
	printers, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateQueue(printers, "psc008", "psc008-sx", true); err != nil {
		t.Fatalf("known student queue rejected: %v", err)
	}
	if err := ValidateQueue(printers, "psc008", "psc008-typo", true); err == nil {
		t.Fatal("unknown queue was accepted")
	}
	var restricted Printer
	for _, printer := range printers {
		if !IsStudentEligible(printer) {
			restricted = printer
			break
		}
	}
	if restricted.ID == "" {
		t.Fatal("fixture catalog has no restricted printer")
	}
	if err := ValidateQueue(printers, restricted.ID, restricted.Queues[0], true); err == nil {
		t.Fatalf("restricted printer %s was accepted for a student", restricted.ID)
	}
}
