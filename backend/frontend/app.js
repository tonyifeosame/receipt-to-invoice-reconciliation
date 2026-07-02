const apiBase = "http://localhost:8080";
const tokenKey = "finance_token";
let dailyChart;
let ocrChart;

function getToken() {
  return localStorage.getItem(tokenKey);
}

function setAuthState(isLoggedIn) {
  document.getElementById("loginSection").classList.toggle("d-none", isLoggedIn);
  document.getElementById("appSection").classList.toggle("d-none", !isLoggedIn);
  document.getElementById("logoutBtn").classList.toggle("d-none", !isLoggedIn);
}

function updateTimestamp() {
  const stamp = new Date().toLocaleString();
  document.getElementById("lastUpdated").textContent = `Last Updated: ${stamp}`;
}

function renderCharts(data) {
  const dailyLabels = (data.daily_stats || []).map((item) => item.date).reverse();
  const matchedValues = (data.daily_stats || []).map((item) => item.matched).reverse();
  const unmatchedValues = (data.daily_stats || []).map((item) => item.unmatched).reverse();

  if (dailyChart) dailyChart.destroy();
  if (ocrChart) ocrChart.destroy();

  const dailyCtx = document.getElementById("dailyChart").getContext("2d");
  dailyChart = new Chart(dailyCtx, {
    type: "bar",
    data: {
      labels: dailyLabels,
      datasets: [
        { label: "Matched", data: matchedValues, backgroundColor: "#198754" },
        { label: "Unmatched", data: unmatchedValues, backgroundColor: "#dc3545" }
      ]
    },
    options: { responsive: true, maintainAspectRatio: false }
  });

  const ocrCtx = document.getElementById("ocrChart").getContext("2d");
  ocrChart = new Chart(ocrCtx, {
    type: "doughnut",
    data: {
      labels: ["Successful", "Needs Review"],
      datasets: [{ data: [85, 15], backgroundColor: ["#0d6efd", "#ffc107"] }]
    },
    options: { responsive: true, maintainAspectRatio: false }
  });
}

async function api(path, options = {}) {
  const headers = options.headers || {};
  if (getToken()) {
    headers.Authorization = `Bearer ${getToken()}`;
  }
  const res = await fetch(`${apiBase}${path}`, { ...options, headers });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || data.message || "Request failed");
  }
  return data;
}

async function loadDashboard() {
  try {
    const data = await api("/dashboard");
    document.getElementById("pendingReviews").textContent = data.pending_reviews || 0;
    document.getElementById("successfulReconciliations").textContent = data.successful_reconciliations || 0;
    document.getElementById("failedReconciliations").textContent = data.failed_reconciliations || 0;
    document.getElementById("recentUploads").textContent = data.recent_uploads || 0;

    renderCharts(data);

    const reviews = data.recent_history || [];
    const reviewList = document.getElementById("reviewList");
    reviewList.innerHTML = reviews.length
      ? reviews.map((item) => `
        <tr>
          <td>${item.receipt_name || "-"}</td>
          <td>${item.invoice_number || "-"}</td>
          <td>72%</td>
          <td><span class="badge bg-warning text-dark">${item.status || "Pending"}</span></td>
          <td>
            <button class="btn btn-sm btn-success me-2" data-action="approve" data-invoice="${item.invoice_number}" data-receipt="${item.receipt_name}">Approve</button>
            <button class="btn btn-sm btn-outline-danger" data-action="reject" data-invoice="${item.invoice_number}" data-receipt="${item.receipt_name}">Reject</button>
          </td>
        </tr>`).join("")
      : '<tr><td colspan="5" class="text-muted">No review items yet.</td></tr>';

    const auditLogs = document.getElementById("auditLogs");
    const logs = data.audit_activity || [];
    auditLogs.innerHTML = logs.length
      ? logs.map((entry) => `<div class="border rounded p-2 mb-2"><div class="fw-bold">${entry.action}</div><div class="small">${entry.description}</div><div class="small text-muted">${entry.user_name}</div></div>`).join("")
      : '<div class="text-muted">No audit activity yet.</div>';

    updateTimestamp();
  } catch (error) {
    console.error(error);
  }
}

async function login(username, password) {
  const data = await api("/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password })
  });
  localStorage.setItem(tokenKey, data.token);
  setAuthState(true);
  loadDashboard();
}

async function uploadReceipt(file) {
  const formData = new FormData();
  formData.append("file", file);
  const statusBox = document.getElementById("uploadStatus");
  const progressWrap = document.getElementById("uploadProgressWrap");
  const progressBar = document.getElementById("uploadProgressBar");

  statusBox.innerHTML = '<div class="alert alert-info">Uploading...</div>';
  progressWrap.classList.remove("d-none");
  progressBar.style.width = "25%";
  progressBar.textContent = "25%";

  await new Promise((resolve) => setTimeout(resolve, 400));
  progressBar.style.width = "65%";
  progressBar.textContent = "65%";
  statusBox.innerHTML = '<div class="alert alert-info">Processing OCR...</div>';

  await new Promise((resolve) => setTimeout(resolve, 400));
  progressBar.style.width = "90%";
  progressBar.textContent = "90%";
  statusBox.innerHTML = '<div class="alert alert-info">Matching Invoice...</div>';

  const data = await api("/receipts/upload", {
    method: "POST",
    body: formData
  });

  progressBar.style.width = "100%";
  progressBar.textContent = "100%";
  statusBox.innerHTML = `<div class="alert alert-success">Completed ✓ ${data.message}</div>`;
  return data;
}

function formatInvoiceResult(data) {
  return [
    `Invoice: ${data.invoice_number || "-"}`,
    `Customer: ${data.customer_name || "-"}`,
    `Amount: ${data.amount ? `₦${data.amount.toLocaleString()}` : "-"}`,
    `Status: ${data.status || "-"}`,
    `Payment Date: ${data.due_date || "-"}`
  ].join("\n");
}

async function searchInvoice(invoiceNumber) {
  const data = await api(`/invoices?invoice_number=${encodeURIComponent(invoiceNumber)}`);
  document.getElementById("invoiceResult").textContent = formatInvoiceResult(data);
}

async function searchPayments(invoiceId) {
  const data = await api(`/payments?invoice_id=${encodeURIComponent(invoiceId)}`);
  document.getElementById("paymentResult").textContent = JSON.stringify(data, null, 2);
}

async function submitReview(action, invoiceNumber, receiptFile) {
  await api("/reviews/decision", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ invoice_number: invoiceNumber, receipt_file: receiptFile, decision: action, reason: "Reviewed from dashboard" })
  });
  loadDashboard();
}

document.getElementById("loginForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const username = document.getElementById("username").value;
  const password = document.getElementById("password").value;
  try {
    await login(username, password);
  } catch (error) {
    alert(error.message);
  }
});

document.getElementById("logoutBtn").addEventListener("click", () => {
  localStorage.removeItem(tokenKey);
  setAuthState(false);
});

document.getElementById("receiptFile").addEventListener("change", (event) => {
  const file = event.target.files[0];
  const previewBox = document.getElementById("previewBox");
  if (!file) {
    previewBox.classList.add("d-none");
    previewBox.innerHTML = "";
    return;
  }
  previewBox.classList.remove("d-none");
  previewBox.innerHTML = `<strong>${file.name}</strong><br /><span class="text-muted">${(file.size / 1024).toFixed(1)} KB</span>`;
});

document.getElementById("uploadForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const file = document.getElementById("receiptFile").files[0];
  if (!file) {
    return;
  }
  try {
    await uploadReceipt(file);
  } catch (error) {
    document.getElementById("uploadStatus").innerHTML = `<div class="alert alert-danger">${error.message}</div>`;
  }
});

document.getElementById("searchInvoiceBtn").addEventListener("click", async () => {
  const value = document.getElementById("invoiceSearch").value;
  if (!value) return;
  try {
    await searchInvoice(value);
  } catch (error) {
    document.getElementById("invoiceResult").textContent = error.message;
  }
});

document.getElementById("searchPaymentsBtn").addEventListener("click", async () => {
  const value = document.getElementById("paymentSearch").value;
  if (!value) return;
  try {
    await searchPayments(value);
  } catch (error) {
    document.getElementById("paymentResult").textContent = error.message;
  }
});

document.addEventListener("click", async (event) => {
  const button = event.target.closest("button[data-action]");
  if (!button) return;
  const action = button.getAttribute("data-action");
  const invoiceNumber = button.getAttribute("data-invoice");
  const receiptFile = button.getAttribute("data-receipt");
  try {
    await submitReview(action, invoiceNumber, receiptFile);
  } catch (error) {
    alert(error.message);
  }
});

document.querySelectorAll(".nav-link[data-view]").forEach((button) => {
  button.addEventListener("click", () => {
    document.querySelectorAll(".nav-link[data-view]").forEach((item) => item.classList.remove("active"));
    button.classList.add("active");
  });
});

if (getToken()) {
  setAuthState(true);
  loadDashboard();
}
