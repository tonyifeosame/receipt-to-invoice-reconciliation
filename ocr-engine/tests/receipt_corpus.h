#ifndef RECEIPT_CORPUS_H
#define RECEIPT_CORPUS_H

// A labelled corpus of receipt texts used both by the unit tests and by the
// accuracy harness. It contains no parser types, so the same corpus can be
// compiled against any version of the parser for a like-for-like comparison.
//
// Ground truth uses the value as printed on the receipt: invoice numbers keep
// their alphabetic prefix (invoices are stored as "INV-001"), dates are
// normalised to YYYY-MM-DD, and amounts are the total actually payable.

#include <string>
#include <vector>

struct ReceiptSample {
    std::string name;
    std::string text;

    std::string invoice;
    double amount;
    std::string date;
    std::string reference;
    std::string customer;
};

inline std::vector<ReceiptSample> receiptCorpus() {
    return {
        // 1. Real Tesseract output captured from a photographed receipt. Note the
        // em dash in "INV—1023" and the misread email, exactly as produced.
        {
            "real-ocr-acme",
            "ACME STORES LTD\nLagos, Nigeria\n\nInvoice: INV\xE2\x80\x94""1023\n"
            "Customer: John Doe Ltd\nDate: 2026-07-03\nReference: TRX238239\n"
            "Tel: 0803-123-4567\n\nEmail: poyOacme.example\n\n"
            "Subtotal: 140,000.00\nVAT: 10,000.00\nTOTAL: 150,000.00\nPAID \xE2\x80\x94 THANK YOU\n",
            "INV-1023", 150000.00, "2026-07-03", "TRX238239", "John Doe Ltd"
        },

        // 2. Canonical layout used by the seed data.
        {
            "iso-date-simple",
            "Invoice: INV-001\nAmount: $1500.00\nDate: 2026-06-29\n"
            "Reference: REF12345\nCustomer: Acme Corp\n",
            "INV-001", 1500.00, "2026-06-29", "REF12345", "Acme Corp"
        },

        // 3. Totals must beat the first number on the page.
        {
            "subtotal-vat-total",
            "MEGA MART\nInvoice No: INV-2048\nBill To: Blue Ocean Ltd\n"
            "Date: 12/07/2026\nSubtotal: 90,000.00\nVAT (7.5%): 6,750.00\n"
            "Grand Total: 96,750.00\nPayment Reference: RRN-889231\n",
            "INV-2048", 96750.00, "2026-07-12", "RRN-889231", "Blue Ocean Ltd"
        },

        // 4. Naira symbol and no decimals.
        {
            "naira-no-decimals",
            "GAMMA TRADE\nRECEIPT NO: RC-7781\nSold To: Delta Foods\n"
            "Payment Date: 22 July 2026\nAmount Paid: \xE2\x82\xA6""212,000\n"
            "Ref: TRX-556677\n",
            "RC-7781", 212000.00, "2026-07-22", "TRX-556677", "Delta Foods"
        },

        // 5. Unambiguous day-first date and a reference with a plain label.
        {
            "day-first-date",
            "Invoice #1027\nCustomer: Nova Retail\nDate: 25/12/2026\n"
            "Total: 43,000.50\nREF: ABC-99001\n",
            "1027", 43000.50, "2026-12-25", "ABC-99001", "Nova Retail"
        },

        // 6. Month name first.
        {
            "month-name-first",
            "Invoice: INV-3312\nBilled To: Tech Solutions Ltd\n"
            "Date: January 15, 2026\nTotal Due: 2,750.50\nReference: PAY0099123\n",
            "INV-3312", 2750.50, "2026-01-15", "PAY0099123", "Tech Solutions Ltd"
        },

        // 7. Due date must not win over the payment date.
        {
            "due-date-distractor",
            "Invoice: INV-4400\nCustomer: StartUp Ventures\nDue Date: 2026-08-30\n"
            "Payment Date: 2026-08-05\nAmount Due: 875.25\nRef No: TX-4400921\n",
            "INV-4400", 875.25, "2026-08-05", "TX-4400921", "StartUp Ventures"
        },

        // 8. European decimal convention.
        {
            "european-decimals",
            "Rechnung: INV-5501\nKunde\nEnterprise Systems GmbH\n"
            "Customer: Enterprise Systems GmbH\nDate: 03.08.2026\n"
            "Total: 3.200,00\nReference: DE-778812\n",
            "INV-5501", 3200.00, "2026-08-03", "DE-778812", "Enterprise Systems GmbH"
        },

        // 9. Everything on one line.
        {
            "single-line",
            "Invoice: INV-6602 Customer: Riverside Traders Date: 2026-05-14 "
            "Total: 12,500.00 Reference: RV-220145\n",
            "INV-6602", 12500.00, "2026-05-14", "RV-220145", "Riverside Traders"
        },

        // 10. Label on its own line, value beneath it.
        {
            "value-on-next-line",
            "PAYMENT ADVICE\nInvoice No: INV-7703\nCustomer:\nHarbour Logistics Ltd\n"
            "Date: 2026-04-02\nAmount Paid: 58,400.00\nReference: HL-330876\n",
            "INV-7703", 58400.00, "2026-04-02", "HL-330876", "Harbour Logistics Ltd"
        },

        // 11. Phone and account numbers must not be mistaken for the amount or
        // the invoice number.
        {
            "contact-noise",
            "SUNSHINE STORES\nTel: 0803-123-4567\nAccount No: 0123456789\n"
            "Invoice: INV-8804\nBill To: Kayode & Sons\nDate: 2026-09-11\n"
            "Total: 7,300.00\nReference: SS-1122334\n",
            "INV-8804", 7300.00, "2026-09-11", "SS-1122334", "Kayode & Sons"
        },

        // 12. Slash separated ISO-like date and a slashed invoice number.
        {
            "slash-invoice",
            "Invoice Number: INV/2026/0091\nCustomer: Palm Grove Foods\n"
            "Date: 2026/03/07\nTotal Payable: 19,999.99\nTransaction Ref: PG-889900\n",
            "INV/2026/0091", 19999.99, "2026-03-07", "PG-889900", "Palm Grove Foods"
        },

        // 13. Lowercase labels and tight spacing.
        {
            "lowercase-labels",
            "invoice:inv-9905\ncustomer:Zenith Hardware\ndate:2026-02-18\n"
            "total:4,250.00\nref:ZH-556677\n",
            "inv-9905", 4250.00, "2026-02-18", "ZH-556677", "Zenith Hardware"
        },

        // 14. Change/tender lines must not outrank the total.
        {
            "cash-tendered",
            "QUICK SHOP\nReceipt No: RC-1201\nCustomer: Walk In Customer\n"
            "Date: 2026-06-01\nTotal: 3,450.00\nCash Tendered: 5,000.00\n"
            "Change: 1,550.00\nRef: QS-090909\n",
            "RC-1201", 3450.00, "2026-06-01", "QS-090909", "Walk In Customer"
        },

        // 15. Two digit year.
        {
            "two-digit-year",
            "Invoice: INV-1300\nPaid By: Coastal Cement\nDate: 09/10/26\n"
            "Total: 88,000.00\nRRN: CC-445566\n",
            "INV-1300", 88000.00, "2026-10-09", "CC-445566", "Coastal Cement"
        },

        // 16. Dashed month name.
        {
            "dashed-month-name",
            "Invoice: INV-1408\nClient: Northwind Traders\nDate: 15-Mar-2026\n"
            "Amount: 6,150.75\nReference: NW-778899\n",
            "INV-1408", 6150.75, "2026-03-15", "NW-778899", "Northwind Traders"
        },

        // 17. Impossible date must be rejected rather than reported.
        {
            "invalid-date",
            "Invoice: INV-1509\nCustomer: Sahara Freight\nDate: 45/13/2026\n"
            "Total: 2,000.00\nReference: SF-101010\n",
            "INV-1509", 2000.00, "", "SF-101010", "Sahara Freight"
        },

        // 18. Sparse receipt: only an invoice and a total.
        {
            "sparse",
            "INV-1610\nTOTAL 950.00\n",
            "INV-1610", 950.00, "", "", ""
        },
    };
}

#endif // RECEIPT_CORPUS_H
