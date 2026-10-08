package catalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed printers.json
var source []byte

type Printer struct {
	ID       string   `json:"id"`
	Location string   `json:"location"`
	Model    string   `json:"model"`
	Banner   string   `json:"banner"`
	Kind     string   `json:"kind"`
	Paper    string   `json:"paper"`
	Access   string   `json:"access"`
	Queues   []string `json:"queues"`
}

func All() ([]Printer, error) {
	var printers []Printer
	if err := json.Unmarshal(source, &printers); err != nil {
		return nil, err
	}
	return printers, nil
}

func Find(printers []Printer, id string) (Printer, bool) {
	for _, printer := range printers {
		if printer.ID == id {
			return printer, true
		}
	}
	return Printer{}, false
}

func FindQueue(printers []Printer, queue string) (Printer, bool) {
	for _, printer := range printers {
		for _, allowed := range printer.Queues {
			if allowed == queue {
				return printer, true
			}
		}
	}
	return Printer{}, false
}

func Modes(printer Printer) []string { return append([]string(nil), printer.Queues...) }

func IsStudentEligible(printer Printer) bool { return printer.Access == "public" }

func Filter(printers []Printer, query string, includeRestricted bool) []Printer {
	query = strings.ToLower(strings.TrimSpace(query))
	var result []Printer
	for _, printer := range printers {
		if !includeRestricted && !IsStudentEligible(printer) {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(strings.Join([]string{printer.ID, printer.Location, printer.Model, printer.Kind, printer.Paper}, " ")), query) {
			result = append(result, printer)
		}
	}
	return result
}

func ValidateQueue(printers []Printer, printerID, queue string, student bool) error {
	printer, ok := Find(printers, printerID)
	if !ok {
		return fmt.Errorf("unknown printer %q", printerID)
	}
	if student && !IsStudentEligible(printer) {
		return fmt.Errorf("printer %s is restricted to %s", printer.ID, printer.Access)
	}
	for _, allowed := range printer.Queues {
		if allowed == queue {
			return nil
		}
	}
	return fmt.Errorf("queue %q is not available at %s", queue, printer.ID)
}
