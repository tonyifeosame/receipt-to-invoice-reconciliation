#include <gtest/gtest.h>
#include "../include/receipt_parser.h"

class ReceiptParserTest : public ::testing::Test {
protected:
    ReceiptParser parser;
};

TEST_F(ReceiptParserTest, ParseValidReceipt) {
    std::string ocrText = "Invoice: INV-001\nAmount: $1500.00\nDate: 2026-06-29\nReference: REF12345\nCustomer: Acme Corp";
    
    ReceiptData data = parser.parseReceipt(ocrText, 95.0);
    
    EXPECT_TRUE(data.isValid);
    EXPECT_EQ(data.invoiceNumber, "INV001");
    EXPECT_DOUBLE_EQ(data.amountPaid, 1500.00);
    EXPECT_EQ(data.paymentDate, "2026-06-29");
    EXPECT_EQ(data.bankReference, "REF12345");
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

TEST_F(ReceiptParserTest, ExtractInvoiceNumber) {
    std::string text = "Invoice Number: INV-12345";
    std::string invoice = parser.extractInvoiceNumber(text);
    
    EXPECT_EQ(invoice, "INV12345");
}

TEST_F(ReceiptParserTest, ExtractAmount) {
    std::string text = "Total Amount: $1,234.56";
    std::string amount = parser.extractAmount(text);
    
    EXPECT_EQ(amount, "1234.56");
}

TEST_F(ReceiptParserTest, ExtractDate) {
    std::string text = "Payment Date: 2026-06-29";
    std::string date = parser.extractDate(text);
    
    EXPECT_EQ(date, "2026-06-29");
}

TEST_F(ReceiptParserTest, ExtractBankReference) {
    std::string text = "Reference: ABC-12345";
    std::string ref = parser.extractBankReference(text);
    
    EXPECT_EQ(ref, "ABC-12345");
}

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

TEST_F(ReceiptParserTest, ValidateDate) {
    EXPECT_TRUE(parser.isValidDate("2026-06-29"));
    EXPECT_TRUE(parser.isValidDate("29/06/2026"));
    EXPECT_FALSE(parser.isValidDate(""));
    EXPECT_FALSE(parser.isValidDate("invalid-date"));
}
