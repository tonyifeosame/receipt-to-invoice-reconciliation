#include <gtest/gtest.h>
#include "../include/receipt_parser.h"
#include "receipt_corpus.h"

class ReceiptParserTest : public ::testing::Test {
protected:
    ReceiptParser parser;
};

// ---------------------------------------------------------------------------
// end to end
// ---------------------------------------------------------------------------

TEST_F(ReceiptParserTest, ParseValidReceipt) {
    std::string ocrText = "Invoice: INV-001\nAmount: $1500.00\nDate: 2026-06-29\nReference: REF12345\nCustomer: Acme Corp";

    ReceiptData data = parser.parseReceipt(ocrText, 95.0);

    EXPECT_TRUE(data.isValid);
    // The printed identifier is preserved, prefix and all: invoices are stored
    // as "INV-001", so reporting "001" could never be matched.
    EXPECT_EQ(data.invoiceNumber, "INV-001");
    EXPECT_DOUBLE_EQ(data.amountPaid, 1500.00);
    EXPECT_EQ(data.paymentDate, "2026-06-29");
    EXPECT_EQ(data.bankReference, "REF12345");
    EXPECT_EQ(data.customerName, "Acme Corp");
    EXPECT_DOUBLE_EQ(data.confidence, 95.0);
    EXPECT_FALSE(data.requiresManualReview);
}

TEST_F(ReceiptParserTest, ParseReceiptWithLowConfidence) {
    std::string ocrText = "Invoice: INV-002\nAmount: $2750.50\nDate: 2026-06-29";

    ReceiptData data = parser.parseReceipt(ocrText, 80.0);

    EXPECT_TRUE(data.isValid);
    EXPECT_DOUBLE_EQ(data.confidence, 80.0);
    EXPECT_TRUE(data.requiresManualReview);
}

TEST_F(ReceiptParserTest, ParseInvalidReceiptNoInvoice) {
    std::string ocrText = "Amount: $1500.00\nDate: 2026-06-29";

    ReceiptData data = parser.parseReceipt(ocrText, 90.0);

    EXPECT_FALSE(data.isValid);
    EXPECT_TRUE(data.invoiceNumber.empty());
}

TEST_F(ReceiptParserTest, ParseInvalidReceiptNoAmount) {
    std::string ocrText = "Invoice: INV-001\nDate: 2026-06-29";

    ReceiptData data = parser.parseReceipt(ocrText, 90.0);

    EXPECT_FALSE(data.isValid);
    EXPECT_DOUBLE_EQ(data.amountPaid, 0.0);
}

// ---------------------------------------------------------------------------
// invoice number
// ---------------------------------------------------------------------------

TEST_F(ReceiptParserTest, ExtractInvoiceNumber) {
    EXPECT_EQ(parser.extractInvoiceNumber("Invoice Number: INV-12345"), "INV-12345");
}

// Regression: the alphabetic prefix used to be dropped, turning INV-1023 into 1023.
TEST_F(ReceiptParserTest, InvoiceNumberKeepsAlphabeticPrefix) {
    EXPECT_EQ(parser.extractInvoiceNumber("Invoice: INV-1023"), "INV-1023");
    EXPECT_EQ(parser.extractInvoiceNumber("INVOICE NO: RC-7781"), "RC-7781");
    EXPECT_EQ(parser.extractInvoiceNumber("Receipt No: RC-1201"), "RC-1201");
}

// Regression: a bare identifier with no separate label word.
TEST_F(ReceiptParserTest, InvoiceNumberFromStandaloneToken) {
    EXPECT_EQ(parser.extractInvoiceNumber("INV-1610\nTOTAL 950.00\n"), "INV-1610");
}

TEST_F(ReceiptParserTest, InvoiceNumberSupportsSlashedFormat) {
    EXPECT_EQ(parser.extractInvoiceNumber("Invoice Number: INV/2026/0091"), "INV/2026/0091");
}

TEST_F(ReceiptParserTest, InvoiceNumberFromHashForm) {
    EXPECT_EQ(parser.extractInvoiceNumber("Invoice #1027\nTotal: 43,000.50"), "1027");
}

// Regression: an em dash from OCR used to defeat the hyphen based patterns.
TEST_F(ReceiptParserTest, InvoiceNumberSurvivesOcrEmDash) {
    EXPECT_EQ(parser.extractInvoiceNumber("Invoice: INV\xE2\x80\x94""1023"), "INV-1023");
}

TEST_F(ReceiptParserTest, InvoiceNumberIgnoresPhoneAndAccountNumbers) {
    std::string text = "Tel: 0803-123-4567\nAccount No: 0123456789\nInvoice: INV-8804\n";
    EXPECT_EQ(parser.extractInvoiceNumber(text), "INV-8804");
}

TEST_F(ReceiptParserTest, InvoiceNumberIsNotADate) {
    std::string text = "Date: 2026-07-03\nTotal: 100.00\n";
    EXPECT_TRUE(parser.extractInvoiceNumber(text).empty());
}

// ---------------------------------------------------------------------------
// amount
// ---------------------------------------------------------------------------

TEST_F(ReceiptParserTest, ExtractAmount) {
    EXPECT_EQ(parser.extractAmount("Total Amount: $1,234.56"), "1234.56");
}

// Regression: the first number anywhere in the text used to win, so an invoice
// number of 1023 was reported as an amount of 102.
TEST_F(ReceiptParserTest, AmountPrefersTotalOverEarlierNumbers) {
    std::string text = "Invoice #1023\nSubtotal: 140,000.00\nVAT: 10,000.00\nTOTAL: 150,000.00\n";
    ReceiptData data = parser.parseReceipt(text, 90.0);
    EXPECT_DOUBLE_EQ(data.amountPaid, 150000.00);
}

TEST_F(ReceiptParserTest, AmountPrefersGrandTotalOverSubtotal) {
    std::string text = "Subtotal: 90,000.00\nVAT (7.5%): 6,750.00\nGrand Total: 96,750.00\n";
    ReceiptData data = parser.parseReceipt(text, 90.0);
    EXPECT_DOUBLE_EQ(data.amountPaid, 96750.00);
}

TEST_F(ReceiptParserTest, AmountIgnoresCashTenderedAndChange) {
    std::string text = "Total: 3,450.00\nCash Tendered: 5,000.00\nChange: 1,550.00\n";
    ReceiptData data = parser.parseReceipt(text, 90.0);
    EXPECT_DOUBLE_EQ(data.amountPaid, 3450.00);
}

TEST_F(ReceiptParserTest, AmountHandlesCurrencySymbolAndNoDecimals) {
    std::string text = "Amount Paid: \xE2\x82\xA6""212,000\n";
    ReceiptData data = parser.parseReceipt(text, 90.0);
    EXPECT_DOUBLE_EQ(data.amountPaid, 212000.00);
}

TEST_F(ReceiptParserTest, AmountHandlesEuropeanDecimals) {
    std::string text = "Total: 3.200,00\n";
    ReceiptData data = parser.parseReceipt(text, 90.0);
    EXPECT_DOUBLE_EQ(data.amountPaid, 3200.00);
}

TEST_F(ReceiptParserTest, AmountIgnoresPhoneNumbers) {
    std::string text = "Tel: 0803-123-4567\nTotal: 7,300.00\n";
    ReceiptData data = parser.parseReceipt(text, 90.0);
    EXPECT_DOUBLE_EQ(data.amountPaid, 7300.00);
}

TEST_F(ReceiptParserTest, AmountIsZeroWhenAbsent) {
    ReceiptData data = parser.parseReceipt("Invoice: INV-1\nCustomer: Nobody\n", 90.0);
    EXPECT_DOUBLE_EQ(data.amountPaid, 0.0);
}

// ---------------------------------------------------------------------------
// date
// ---------------------------------------------------------------------------

TEST_F(ReceiptParserTest, ExtractDate) {
    EXPECT_EQ(parser.extractDate("Payment Date: 2026-06-29"), "2026-06-29");
}

// Regression: an ambiguous d/m/y pattern was tried before ISO, so a full ISO
// date lost its century and became "26-06-29".
TEST_F(ReceiptParserTest, IsoDateIsNotTruncated) {
    EXPECT_EQ(parser.extractDate("Date: 2026-07-03"), "2026-07-03");
    EXPECT_EQ(parser.extractDate("Date: 2026/03/07"), "2026-03-07");
}

TEST_F(ReceiptParserTest, DateNormalisesDayFirstFormat) {
    EXPECT_EQ(parser.extractDate("Date: 25/12/2026"), "2026-12-25");
    EXPECT_EQ(parser.extractDate("Date: 03.08.2026"), "2026-08-03");
}

TEST_F(ReceiptParserTest, DateNormalisesMonthNames) {
    EXPECT_EQ(parser.extractDate("Date: January 15, 2026"), "2026-01-15");
    EXPECT_EQ(parser.extractDate("Payment Date: 22 July 2026"), "2026-07-22");
    EXPECT_EQ(parser.extractDate("Date: 15-Mar-2026"), "2026-03-15");
}

TEST_F(ReceiptParserTest, DateExpandsTwoDigitYear) {
    EXPECT_EQ(parser.extractDate("Date: 09/10/26"), "2026-10-09");
}

TEST_F(ReceiptParserTest, DatePrefersPaymentDateOverDueDate) {
    std::string text = "Due Date: 2026-08-30\nPayment Date: 2026-08-05\n";
    EXPECT_EQ(parser.extractDate(text), "2026-08-05");
}

TEST_F(ReceiptParserTest, DateRejectsImpossibleValues) {
    EXPECT_TRUE(parser.extractDate("Date: 45/13/2026").empty());
    EXPECT_TRUE(parser.extractDate("Date: 2026-02-31").empty());
    EXPECT_FALSE(parser.extractDate("Date: 2028-02-29").empty()); // 2028 is a leap year
}

TEST_F(ReceiptParserTest, ValidateDate) {
    EXPECT_TRUE(parser.isValidDate("2026-06-29"));
    EXPECT_TRUE(parser.isValidDate("29/06/2026"));
    EXPECT_FALSE(parser.isValidDate(""));
    EXPECT_FALSE(parser.isValidDate("invalid-date"));
}

// ---------------------------------------------------------------------------
// reference
// ---------------------------------------------------------------------------

// Regression: "ref" matched inside the word "Reference" and the parser returned
// the remainder of the label, "erence", as the reference.
TEST_F(ReceiptParserTest, ExtractBankReference) {
    EXPECT_EQ(parser.extractBankReference("Reference: ABC-12345"), "ABC-12345");
}

TEST_F(ReceiptParserTest, ReferenceSupportsCommonLabels) {
    EXPECT_EQ(parser.extractBankReference("Ref: TRX-556677"), "TRX-556677");
    EXPECT_EQ(parser.extractBankReference("Ref No: TX-4400921"), "TX-4400921");
    EXPECT_EQ(parser.extractBankReference("Payment Reference: RRN-889231"), "RRN-889231");
    EXPECT_EQ(parser.extractBankReference("Transaction Ref: PG-889900"), "PG-889900");
    EXPECT_EQ(parser.extractBankReference("RRN: CC-445566"), "CC-445566");
}

TEST_F(ReceiptParserTest, ReferenceKeepsSeparators) {
    EXPECT_EQ(parser.extractBankReference("Reference: TRX238239"), "TRX238239");
    EXPECT_EQ(parser.extractBankReference("Reference: SS-1122334"), "SS-1122334");
}

TEST_F(ReceiptParserTest, ReferenceAbsentIsEmpty) {
    EXPECT_TRUE(parser.extractBankReference("Total: 100.00\n").empty());
}

// ---------------------------------------------------------------------------
// customer
// ---------------------------------------------------------------------------

// Regression: the character class included newlines, so the customer name ran
// on into the next line and became "John Doe Ltd\nDate".
TEST_F(ReceiptParserTest, CustomerNameStopsAtEndOfLine) {
    std::string text = "Customer: John Doe Ltd\nDate: 2026-07-03\n";
    EXPECT_EQ(parser.extractCustomerName(text), "John Doe Ltd");
}

TEST_F(ReceiptParserTest, CustomerNameStopsAtNextLabelOnSameLine) {
    std::string text = "Invoice: INV-6602 Customer: Riverside Traders Date: 2026-05-14\n";
    EXPECT_EQ(parser.extractCustomerName(text), "Riverside Traders");
}

TEST_F(ReceiptParserTest, CustomerNameSupportsCommonLabels) {
    EXPECT_EQ(parser.extractCustomerName("Bill To: Blue Ocean Ltd\n"), "Blue Ocean Ltd");
    EXPECT_EQ(parser.extractCustomerName("Sold To: Delta Foods\n"), "Delta Foods");
    EXPECT_EQ(parser.extractCustomerName("Paid By: Coastal Cement\n"), "Coastal Cement");
    EXPECT_EQ(parser.extractCustomerName("Client: Northwind Traders\n"), "Northwind Traders");
}

TEST_F(ReceiptParserTest, CustomerNameReadsValueFromNextLine) {
    std::string text = "Customer:\nHarbour Logistics Ltd\nDate: 2026-04-02\n";
    EXPECT_EQ(parser.extractCustomerName(text), "Harbour Logistics Ltd");
}

TEST_F(ReceiptParserTest, CustomerNameKeepsPunctuationInsideName) {
    EXPECT_EQ(parser.extractCustomerName("Bill To: Kayode & Sons\n"), "Kayode & Sons");
}

TEST_F(ReceiptParserTest, CustomerNameAbsentIsEmpty) {
    EXPECT_TRUE(parser.extractCustomerName("Invoice: INV-1\nTotal: 10.00\n").empty());
}

// ---------------------------------------------------------------------------
// contact details are still extracted
// ---------------------------------------------------------------------------

// Regression: the whole match was returned, so the label came with the value.
TEST_F(ReceiptParserTest, PhoneNumberExcludesLabel) {
    EXPECT_EQ(parser.extractPhoneNumber("Tel: 0803-123-4567"), "08031234567");
}

TEST_F(ReceiptParserTest, EmailIsStillExtracted) {
    EXPECT_EQ(parser.extractEmail("Email: billing@acme.example"), "billing@acme.example");
}

// ---------------------------------------------------------------------------
// validation helpers
// ---------------------------------------------------------------------------

TEST_F(ReceiptParserTest, ValidateInvoiceNumber) {
    EXPECT_TRUE(parser.isValidInvoiceNumber("INV-001"));
    EXPECT_TRUE(parser.isValidInvoiceNumber("INV123"));
    EXPECT_FALSE(parser.isValidInvoiceNumber(""));
    EXPECT_FALSE(parser.isValidInvoiceNumber("AB"));
}

TEST_F(ReceiptParserTest, ValidateAmount) {
    EXPECT_TRUE(parser.isValidAmount("1500.00"));
    EXPECT_TRUE(parser.isValidAmount("$1500.00"));
    EXPECT_FALSE(parser.isValidAmount(""));
    EXPECT_FALSE(parser.isValidAmount("invalid"));
}

// ---------------------------------------------------------------------------
// corpus level expectations
// ---------------------------------------------------------------------------

// Every labelled receipt in the corpus must be extracted exactly.
TEST_F(ReceiptParserTest, CorpusIsParsedExactly) {
    for (const auto& sample : receiptCorpus()) {
        ReceiptData data = parser.parseReceipt(sample.text, 90.0);

        EXPECT_EQ(data.invoiceNumber, sample.invoice) << "invoice, sample: " << sample.name;
        EXPECT_NEAR(data.amountPaid, sample.amount, 0.005) << "amount, sample: " << sample.name;
        EXPECT_EQ(data.paymentDate, sample.date) << "date, sample: " << sample.name;
        EXPECT_EQ(data.bankReference, sample.reference) << "reference, sample: " << sample.name;
        EXPECT_EQ(data.customerName, sample.customer) << "customer, sample: " << sample.name;
    }
}

// The parser must never be handed responsibility for the raw text: callers keep
// reporting it, so a receipt that yields no fields still returns cleanly.
TEST_F(ReceiptParserTest, UnparseableTextDoesNotThrow) {
    EXPECT_NO_THROW({
        ReceiptData data = parser.parseReceipt("", 0.0);
        EXPECT_FALSE(data.isValid);
    });
    EXPECT_NO_THROW(parser.parseReceipt("!!! ??? ***\n\n\n", 10.0));
}
