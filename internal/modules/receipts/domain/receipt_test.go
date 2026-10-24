package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

var today = dates.New(2026, time.October, 24)

func amount(value string) money.Amount {
	parsed, err := money.ParseAmount(value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func validReceipt() Receipt {
	return Receipt{
		Series: "e001", Number: "00109", IssueDate: dates.New(2026, time.September, 7),
		Client: Client{Name: "  SISTEMAS   Y SOLUCIONES S.A.C. ", DocType: "ruc", DocNumber: "20492684679"},
		Gross:  amount("2000.00"), Withholding: amount("0.00"), Source: SourceXML,
	}
}

func codesOf(t *testing.T, err error) []string {
	t.Helper()
	var validation *ValidationError
	require.ErrorAs(t, err, &validation)
	codes := make([]string, 0, len(validation.Fields))
	for _, f := range validation.Fields {
		codes = append(codes, f.Field+":"+f.Code)
	}
	return codes
}

func TestNormalizeCleansTheReceipt(t *testing.T) {
	r := validReceipt()
	r.Normalize()
	assert.Equal(t, "E001", r.Series)
	assert.Equal(t, "109", r.Number)
	assert.Equal(t, "SISTEMAS Y SOLUCIONES S.A.C.", r.Client.Name)
	assert.Equal(t, "RUC", r.Client.DocType)
	assert.Equal(t, "E001-109", r.Code())
	assert.NoError(t, r.Validate(today))
}

func TestNetIsGrossMinusWithholding(t *testing.T) {
	r := validReceipt()
	r.Gross, r.Withholding = amount("3500.00"), amount("280.00")
	assert.Equal(t, "3220.00", r.Net().String())
}

func TestValidateReportsEachRule(t *testing.T) {
	collected := dates.New(2026, time.September, 1)
	r := Receipt{
		Series: "", Number: "12a", IssueDate: today.AddDays(1),
		Client: Client{Name: "", DocType: DocDNI, DocNumber: "123"},
		Gross:  money.NewAmount(decimal.Zero), Description: strings.Repeat("x", 501),
		CollectedOn: &collected, Source: "other",
	}
	assert.Equal(t, []string{
		"series:SERIES_REQUIRED", "number:NUMBER_INVALID", "issue_date:DATE_IN_FUTURE",
		"client.name:CLIENT_NAME_REQUIRED", "client.doc_number:DNI_INVALID", "gross:AMOUNT_INVALID",
		"description:DESCRIPTION_TOO_LONG", "collected_on:COLLECTED_BEFORE_ISSUE", "source:SOURCE_INVALID",
	}, codesOf(t, r.Validate(today)))
}

func TestWithholdingMustBeBelowGross(t *testing.T) {
	r := validReceipt()
	r.Normalize()
	r.Withholding = r.Gross
	assert.Equal(t, []string{"withholding:WITHHOLDING_INVALID"}, codesOf(t, r.Validate(today)))
}

func TestRucAndOtherDocuments(t *testing.T) {
	r := validReceipt()
	r.Normalize()
	r.Client.DocNumber = "1049268467"
	assert.Equal(t, []string{"client.doc_number:RUC_INVALID"}, codesOf(t, r.Validate(today)))

	r.Client = Client{Name: "Cliente del exterior", DocType: DocOther, DocNumber: "P-123"}
	assert.NoError(t, r.Validate(today))
}

const sunatXML = `<?xml version="1.0" encoding="UTF-8" standalone="no"?><?xml-stylesheet type="text/xsl" href="recibo.xsl"?>
<Invoice xmlns="urn:oasis:names:specification:ubl:schema:xsd:Invoice-2" xmlns:cbc="urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2" xmlns:cac="urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2" xmlns:ext="urn:oasis:names:specification:ubl:schema:xsd:CommonExtensionComponents-2">
	<ext:UBLExtensions><ext:UBLExtension><ext:ExtensionContent><Signature><ID>firma</ID></Signature></ext:ExtensionContent></ext:UBLExtension></ext:UBLExtensions>
	<cbc:ID>E001-109</cbc:ID>
	<cbc:IssueDate>2026-09-07</cbc:IssueDate>
	<cac:OrderReference><cbc:ID>E001-999</cbc:ID></cac:OrderReference>
</Invoice>`

func TestXMLReceiptCodeReadsTheRootID(t *testing.T) {
	code, err := XMLReceiptCode([]byte(sunatXML))
	require.NoError(t, err)
	assert.Equal(t, "E001-109", code)

	_, err = XMLReceiptCode([]byte("%PDF-1.4"))
	assert.Error(t, err)
}

func TestCheckFile(t *testing.T) {
	r := validReceipt()
	r.Normalize()
	assert.NoError(t, CheckFile(KindXML, []byte(sunatXML), r))
	assert.NoError(t, CheckFile(KindPDF, []byte("%PDF-1.7\n..."), r))

	other := r
	other.Number = "58"
	assert.ErrorIs(t, CheckFile(KindXML, []byte(sunatXML), other), ErrXMLMismatch)
	assert.ErrorIs(t, CheckFile(KindPDF, []byte("hola"), r), ErrFileInvalid)
	assert.ErrorIs(t, CheckFile(KindXML, []byte("no es xml"), r), ErrFileInvalid)
	assert.ErrorIs(t, CheckFile(KindPDF, make([]byte, MaxPDFBytes+1), r), ErrFileInvalid)
}

func TestStorageKeyAndFileName(t *testing.T) {
	user, receipt, doc := uuid.New(), uuid.New(), uuid.New()
	key := StorageKey(user, receipt, doc, KindPDF)
	assert.True(t, strings.HasPrefix(key, UserPrefix(user)))
	assert.True(t, strings.HasSuffix(key, doc.String()+".pdf"))

	assert.Equal(t, "recibo.pdf", CleanFileName(`C:\Users\joel\recibo.pdf`, KindPDF))
	assert.Equal(t, "recibo.xml", CleanFileName("  ", KindXML))
	assert.Equal(t, "evil.xml", CleanFileName("../../evil.xml", KindXML))
}
