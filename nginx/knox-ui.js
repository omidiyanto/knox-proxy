/**
 * ══════════════════════════════════════════════════════════════════════════════
 * KNOX UI — JIT Access Request & Ticket Management
 * ══════════════════════════════════════════════════════════════════════════════
 * Pure vanilla JS — zero external dependencies.
 * Injected by Nginx into n8n's UI via sub_filter.
 */

(function () {
  'use strict';

  // ── Toast Notification System ────────────────────────────────────────────

  function initToastContainer() {
    if (document.getElementById('knox-toast-container')) return;
    var c = document.createElement('div');
    c.id = 'knox-toast-container';
    c.className = 'knox-toast-container';
    document.body.appendChild(c);
  }

  function showToast(type, message) {
    initToastContainer();
    var container = document.getElementById('knox-toast-container');
    var toast = document.createElement('div');
    toast.className = 'knox-toast knox-toast-' + type;
    toast.textContent = (type === 'success' ? '✓ ' : '✕ ') + message;
    container.appendChild(toast);
    setTimeout(function () {
      toast.classList.add('knox-toast-dismiss');
      setTimeout(function () { toast.remove(); }, 300);
    }, 4000);
  }

  // ── Utility Functions ────────────────────────────────────────────────────

  function detectWorkflowID() {
    var match = window.location.pathname.match(/\/workflow\/([a-zA-Z0-9_-]+)/);
    return match ? match[1] : '';
  }

  function formatLocalDatetime(date) {
    var y = date.getFullYear();
    var mo = String(date.getMonth() + 1).padStart(2, '0');
    var d = String(date.getDate()).padStart(2, '0');
    var h = String(date.getHours()).padStart(2, '0');
    var mi = String(date.getMinutes()).padStart(2, '0');
    return y + '-' + mo + '-' + d + 'T' + h + ':' + mi;
  }

  function localToISO(localVal) {
    if (!localVal) return '';
    var d = new Date(localVal);
    return isNaN(d.getTime()) ? '' : d.toISOString();
  }

  function formatDisplayDate(isoStr) {
    if (!isoStr) return '';
    var d = new Date(isoStr);
    if (isNaN(d.getTime())) return '';
    var mo = String(d.getMonth() + 1).padStart(2, '0');
    var da = String(d.getDate()).padStart(2, '0');
    var h = String(d.getHours()).padStart(2, '0');
    var m = String(d.getMinutes()).padStart(2, '0');
    return da + '/' + mo + ' ' + h + ':' + m;
  }

  function calcDuration(startStr, endStr) {
    if (!startStr || !endStr) return { text: '--', warn: false };
    var start = new Date(startStr);
    var end = new Date(endStr);
    var diff = end - start;
    if (diff <= 0) return { text: 'Invalid range', warn: true };
    var days = Math.floor(diff / 86400000);
    var hours = Math.floor((diff % 86400000) / 3600000);
    var mins = Math.floor((diff % 3600000) / 60000);
    var parts = [];
    if (days > 0) parts.push(days + 'd');
    if (hours > 0) parts.push(hours + 'h');
    parts.push(mins + 'm');
    return { text: parts.join(' '), warn: days > 7 || (days === 7 && (hours > 0 || mins > 0)) };
  }

  function getStatusBadge(status) {
    return '<span class="knox-status-badge knox-status-' + status + '">' + status + '</span>';
  }

  // ── Modal HTML Builder ───────────────────────────────────────────────────

  function buildModalHTML() {
    var now = new Date();
    var endDefault = new Date(now.getTime() + 86400000); // +1 day
    var nowStr = formatLocalDatetime(now);
    var endStr = formatLocalDatetime(endDefault);
    var wfId = detectWorkflowID();
    var autoClass = wfId ? ' knox-auto-filled' : '';

    return '' +
      '<div class="knox-modal-header">' +
      '<span class="knox-modal-title">Request JIT Access</span>' +
      '<button class="knox-modal-close" id="knox-close-btn" title="Close">&times;</button>' +
      '</div>' +
      '<div class="knox-tabs">' +
      '<button class="knox-tab knox-tab-active" data-knox-tab="request">New Request</button>' +
      '<button class="knox-tab" data-knox-tab="tickets">My Tickets</button>' +
      '</div>' +
      '<div class="knox-modal-body">' +
      '<!-- New Request Tab -->' +
      '<div class="knox-tab-content knox-tab-visible" id="knox-tab-request">' +
      '<div class="knox-form-group">' +
      '<label class="knox-label">Workflow ID</label>' +
      '<input type="text" class="knox-input' + autoClass + '" id="knox-workflow-id" ' +
      'placeholder="e.g., AUqnl095YhDTo47d" value="' + wfId + '">' +
      '</div>' +
      '<div class="knox-form-group">' +
      '<label class="knox-label">Access Type</label>' +
      '<div class="knox-checkbox-group">' +
      '<label class="knox-checkbox-label">' +
      '<input type="checkbox" name="knox-access" value="run" checked> ' +
      '<span>Run</span>' +
      '</label>' +
      (window.__KNOX_JIT_EDIT__ ?
        '<label class="knox-checkbox-label">' +
        '<input type="checkbox" name="knox-access" value="edit"> ' +
        '<span>Edit</span>' +
        '</label>' : '') +
      '</div>' +
      '</div>' +
      '<div class="knox-form-group">' +
      '<label class="knox-label">Request Period</label>' +
      '<div class="knox-date-row">' +
      '<div>' +
      '<input type="datetime-local" class="knox-input" id="knox-period-start" value="' + nowStr + '">' +
      '</div>' +
      '<div>' +
      '<input type="datetime-local" class="knox-input" id="knox-period-end" value="' + endStr + '">' +
      '</div>' +
      '</div>' +
      '<div class="knox-duration-badge" id="knox-duration-badge">1d 0h 0m</div>' +
      '</div>' +
      '<div class="knox-form-group">' +
      '<label class="knox-label">Description</label>' +
      '<textarea class="knox-textarea" id="knox-description" ' +
      'placeholder="Reason for access request (min 10 characters)..." rows="3"></textarea>' +
      '</div>' +
      '</div>' +
      '<!-- My Tickets Tab -->' +
      '<div class="knox-tab-content" id="knox-tab-tickets">' +
      '<div class="knox-loading" id="knox-tickets-loading">Loading tickets...</div>' +
      '<div id="knox-tickets-content"></div>' +
      '</div>' +
      '</div>' +
      '<div class="knox-modal-footer" id="knox-modal-footer">' +
      '<button class="knox-btn knox-btn-secondary" id="knox-cancel-btn">Cancel</button>' +
      '<button class="knox-btn knox-btn-primary" id="knox-submit-btn">Submit Request</button>' +
      '</div>';
  }

  // ── Modal Controller ─────────────────────────────────────────────────────

  var overlay = null;
  var modal = null;

  function createModal() {
    if (overlay) return;

    overlay = document.createElement('div');
    overlay.className = 'knox-modal-overlay';
    overlay.id = 'knox-modal-overlay';

    modal = document.createElement('div');
    modal.className = 'knox-modal';
    modal.innerHTML = buildModalHTML();

    overlay.appendChild(modal);
    document.body.appendChild(overlay);

    // Event listeners
    document.getElementById('knox-close-btn').addEventListener('click', closeModal);
    document.getElementById('knox-cancel-btn').addEventListener('click', closeModal);
    document.getElementById('knox-submit-btn').addEventListener('click', handleSubmit);

    overlay.addEventListener('click', function (e) {
      if (e.target === overlay) closeModal();
    });

    // Tab switching
    var tabs = modal.querySelectorAll('.knox-tab');
    for (var i = 0; i < tabs.length; i++) {
      tabs[i].addEventListener('click', function () {
        switchTab(this.getAttribute('data-knox-tab'));
      });
    }

    // Duration live-calculation
    var startInput = document.getElementById('knox-period-start');
    var endInput = document.getElementById('knox-period-end');
    startInput.addEventListener('change', updateDuration);
    endInput.addEventListener('change', updateDuration);

    // Keyboard
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && overlay && overlay.classList.contains('knox-visible')) {
        closeModal();
      }
    });
  }

  function openModal(tab) {
    createModal();

    // Refresh workflow ID on open (user may have navigated)
    var wfInput = document.getElementById('knox-workflow-id');
    var detectedId = detectWorkflowID();
    if (detectedId && wfInput) {
      wfInput.value = detectedId;
      wfInput.classList.add('knox-auto-filled');
    }

    // Refresh dates
    var now = new Date();
    var endDefault = new Date(now.getTime() + 86400000);
    document.getElementById('knox-period-start').value = formatLocalDatetime(now);
    document.getElementById('knox-period-end').value = formatLocalDatetime(endDefault);
    updateDuration();

    // Show
    overlay.classList.add('knox-visible');
    switchTab(tab || 'request');

    if (tab === 'tickets') {
      loadTickets();
    }
  }

  function closeModal() {
    if (overlay) {
      overlay.classList.remove('knox-visible');
    }
  }

  function switchTab(tabName) {
    // Update tab buttons
    var tabs = modal.querySelectorAll('.knox-tab');
    for (var i = 0; i < tabs.length; i++) {
      tabs[i].classList.toggle('knox-tab-active', tabs[i].getAttribute('data-knox-tab') === tabName);
    }

    // Update tab contents
    document.getElementById('knox-tab-request').classList.toggle('knox-tab-visible', tabName === 'request');
    document.getElementById('knox-tab-tickets').classList.toggle('knox-tab-visible', tabName === 'tickets');

    // Show/hide footer (only for request tab)
    var footer = document.getElementById('knox-modal-footer');
    footer.style.display = tabName === 'request' ? 'flex' : 'none';

    // Load tickets when switching to tickets tab
    if (tabName === 'tickets') {
      loadTickets();
    }
  }

  // ── Duration Calculator ──────────────────────────────────────────────────

  function updateDuration() {
    var start = document.getElementById('knox-period-start').value;
    var end = document.getElementById('knox-period-end').value;
    var badge = document.getElementById('knox-duration-badge');
    var result = calcDuration(start, end);
    badge.textContent = result.text;
    badge.classList.toggle('knox-duration-warning', result.warn);
  }

  // ── Form Submission ──────────────────────────────────────────────────────

  function handleSubmit() {
    var workflowId = document.getElementById('knox-workflow-id').value.trim();
    var periodStart = localToISO(document.getElementById('knox-period-start').value);
    var periodEnd = localToISO(document.getElementById('knox-period-end').value);
    var description = document.getElementById('knox-description').value.trim();

    // Gather checked access types
    var accessType = [];
    if (!window.__KNOX_JIT_EDIT__) {
      // Edit checkbox is hidden — force run-only
      accessType = ['run'];
    } else {
      var accessBoxes = document.querySelectorAll('input[name="knox-access"]:checked');
      for (var i = 0; i < accessBoxes.length; i++) {
        accessType.push(accessBoxes[i].value);
      }
    }

    // Client-side validation
    var errors = [];
    if (!workflowId) errors.push('Workflow ID is required');
    if (accessType.length === 0) errors.push('Select at least one access type');
    if (!periodStart || !periodEnd) errors.push('Both start and end dates are required');
    if (description.length < 10) errors.push('Description must be at least 10 characters');

    var dur = calcDuration(
      document.getElementById('knox-period-start').value,
      document.getElementById('knox-period-end').value
    );
    if (dur.warn) errors.push('Duration exceeds maximum allowed (7 days)');

    if (errors.length > 0) {
      showToast('error', errors[0]);
      return;
    }

    // Disable submit
    var submitBtn = document.getElementById('knox-submit-btn');
    submitBtn.disabled = true;
    submitBtn.innerHTML = '<span class="knox-spinner"></span>Submitting...';

    fetch('/knox-api/request-jit', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        workflow_id: workflowId,
        access_type: accessType,
        period_start: periodStart,
        period_end: periodEnd,
        description: description
      })
    })
      .then(function (resp) {
        return resp.json().then(function (data) {
          return { ok: resp.ok, status: resp.status, data: data };
        });
      })
      .then(function (result) {
        if (result.ok) {
          var tn = result.data.ticket ? result.data.ticket.ticket_number : 'submitted';
          showToast('success', 'Ticket ' + tn + ' created successfully!');
          closeModal();
          // Reset form
          document.getElementById('knox-description').value = '';
        } else {
          var msg = result.data.message || result.data.error || 'Request failed';
          showToast('error', msg);
        }
      })
      .catch(function () {
        showToast('error', 'Network error. Please try again.');
      })
      .finally(function () {
        submitBtn.disabled = false;
        submitBtn.innerHTML = 'Submit Request';
      });
  }

  // ── Ticket History ───────────────────────────────────────────────────────

  function loadTickets() {
    var loadingEl = document.getElementById('knox-tickets-loading');
    var contentEl = document.getElementById('knox-tickets-content');
    if (loadingEl) loadingEl.style.display = 'block';
    if (contentEl) contentEl.innerHTML = '';

    fetch('/knox-api/tickets')
      .then(function (resp) { return resp.json(); })
      .then(function (data) {
        if (loadingEl) loadingEl.style.display = 'none';
        renderTicketTable(data.tickets || [], contentEl);
      })
      .catch(function () {
        if (loadingEl) loadingEl.style.display = 'none';
        if (contentEl) contentEl.innerHTML = '<div class="knox-empty-state">' +
          '<div class="knox-empty-state-icon">⚠️</div>' +
          '<div class="knox-empty-state-text">Failed to load tickets</div></div>';
      });
  }

  function renderTicketTable(tickets, container) {
    if (!container) return;

    if (tickets.length === 0) {
      container.innerHTML = '<div class="knox-empty-state">' +
        '<div class="knox-empty-state-icon">📋</div>' +
        '<div class="knox-empty-state-text">No tickets yet. Submit your first request!</div></div>';
      return;
    }

    var html = '<table class="knox-ticket-table">' +
      '<thead><tr>' +
      '<th>Ticket</th><th>Workflow</th><th>Type</th><th>Period</th><th>Duration</th><th>Status</th><th>Action</th>' +
      '</tr></thead><tbody>';

    for (var i = 0; i < tickets.length; i++) {
      var t = tickets[i];
      var d = calcDuration(t.period_start, t.period_end);
      var actionBtn = '';
      if (t.status === 'requested') {
        actionBtn = '<button class="knox-btn knox-btn-secondary" style="padding: 4px 8px; font-size: 11px;" onclick="window.knoxCancelTicket(\'' + t.id + '\')">Cancel</button>';
      }

      var periodStr = formatDisplayDate(t.period_start) + ' - ' + formatDisplayDate(t.period_end);

      html += '<tr>' +
        '<td class="knox-td-mono">' + escapeHtml(t.ticket_number) + '</td>' +
        '<td class="knox-td-mono">' + escapeHtml(t.workflow_id) + '</td>' +
        '<td>' + escapeHtml(t.access_type) + '</td>' +
        '<td class="knox-td-mono" style="font-size: 10px; white-space: nowrap;">' + periodStr + '</td>' +
        '<td>' + d.text + '</td>' +
        '<td>' + getStatusBadge(t.status) + '</td>' +
        '<td>' + actionBtn + '</td>' +
        '</tr>';
    }

    html += '</tbody></table>';
    container.innerHTML = html;
  }

  function escapeHtml(str) {
    if (!str) return '';
    var div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  // Exposed to global scope for inline onclick handlers
  window.knoxCancelTicket = function (ticketId) {
    if (!confirm('Are you sure you want to cancel this ticket?')) return;

    var loadingEl = document.getElementById('knox-tickets-loading');
    if (loadingEl) loadingEl.style.display = 'block';

    fetch('/knox-api/tickets/' + ticketId + '/cancel', { method: 'PATCH' })
      .then(function (resp) {
        if (!resp.ok) throw new Error('Failed to cancel');
        return resp.json();
      })
      .then(function (data) {
        showToast('success', 'Ticket canceled successfully');
        loadTickets();
      })
      .catch(function (err) {
        if (loadingEl) loadingEl.style.display = 'none';
        showToast('error', 'Failed to cancel ticket');
      });
  };
  // ── User Info Modal ──────────────────────────────────────────────────────

  function openUserInfoModal() {
    fetch('/knox-api/user-info')
      .then(function (resp) { return resp.json(); })
      .then(function (data) {
        showUserInfoBox(data);
      })
      .catch(function () {
        showToast('error', 'Failed to load user info');
      });
  }

  function showUserInfoBox(data) {
    var uiOverlay = document.getElementById('knox-userinfo-overlay');

    if (!uiOverlay) {
      uiOverlay = document.createElement('div');
      uiOverlay.className = 'knox-modal-overlay';
      uiOverlay.id = 'knox-userinfo-overlay';

      var modal = document.createElement('div');
      modal.className = 'knox-modal';
      modal.style.maxWidth = '400px';

      uiOverlay.appendChild(modal);
      document.body.appendChild(uiOverlay);

      uiOverlay.addEventListener('click', function (e) {
        if (e.target === uiOverlay) uiOverlay.classList.remove('knox-visible');
      });
    }

    var modal = uiOverlay.querySelector('.knox-modal');
    modal.innerHTML = '' +
      '<div class="knox-modal-header">' +
      '<span class="knox-modal-title" style="font-size:16px;">User Info</span>' +
      '<button class="knox-modal-close" id="knox-ui-close-btn" title="Close">&times;</button>' +
      '</div>' +
      '<div class="knox-modal-body" style="padding-bottom: 24px;">' +
      '<div class="knox-form-group">' +
      '<label class="knox-label">Display Name</label>' +
      '<div class="knox-input knox-auto-filled">' + escapeHtml(data.display_name || '-') + '</div>' +
      '</div>' +
      '<div class="knox-form-group">' +
      '<label class="knox-label">Email</label>' +
      '<div class="knox-input knox-auto-filled">' + escapeHtml(data.email || '-') + '</div>' +
      '</div>' +
      '<div class="knox-form-group" style="margin-bottom:0;">' +
      '<label class="knox-label">Team</label>' +
      '<div class="knox-input knox-auto-filled">' + escapeHtml(data.team || '-') + '</div>' +
      '</div>' +
      '</div>';

    document.getElementById('knox-ui-close-btn').addEventListener('click', function () {
      uiOverlay.classList.remove('knox-visible');
    });
    setTimeout(function () {
      uiOverlay.classList.add('knox-visible');
    }, 10);
  }

  // ── Sidebar Menu Injection ───────────────────────────────────────────────

  function injectMenuItems() {
    if (document.getElementById('knox-menu-userinfo')) return;

    // Find the custom logout button that was injected by nginx
    var logoutBtn = document.getElementById('custom-logout-btn');
    if (!logoutBtn || !logoutBtn.parentNode) return;

    // 1. Buat menu "JIT Access" (only if enabled via env var)
    var requestItem = null;
    if (window.__KNOX_JIT__) {
      requestItem = logoutBtn.cloneNode(true);
      requestItem.id = 'knox-menu-request';
      requestItem.className = (requestItem.className || '') + ' knox-menu-item';
      if (requestItem.href) requestItem.href = '#';

      var svgs = requestItem.querySelectorAll('svg');
      if (svgs.length > 0) {
        var svg = svgs[0];
        svg.innerHTML = '<path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"></path>';
        svg.setAttribute('viewBox', '0 0 24 24');
        svg.setAttribute('fill', 'none');
        svg.setAttribute('stroke', 'currentColor');
        svg.setAttribute('stroke-width', '2');
        svg.setAttribute('stroke-linecap', 'round');
        svg.setAttribute('stroke-linejoin', 'round');
        for (var i = 1; i < svgs.length; i++) svgs[i].remove();
      }

      var walker = document.createTreeWalker(requestItem, NodeFilter.SHOW_TEXT, null, false);
      var textNode;
      while (textNode = walker.nextNode()) {
        if (textNode.nodeValue.trim() === 'Logout') textNode.nodeValue = 'JIT Access';
      }

      var badges = requestItem.querySelectorAll('[class*="badge"], [class*="indicator"], [class*="dot"], [class*="update"]');
      for (var b = 0; b < badges.length; b++) badges[b].remove();

      requestItem.addEventListener('click', function (e) {
        e.preventDefault(); e.stopPropagation(); openModal('request');
      });
    }

    // 2. Buat menu "User Info"
    var userInfoItem = logoutBtn.cloneNode(true);
    userInfoItem.id = 'knox-menu-userinfo';
    userInfoItem.className = (userInfoItem.className || '') + ' knox-menu-item';
    if (userInfoItem.href) userInfoItem.href = '#';

    // Ubah ikon menjadi ikon user / orang
    var uSvgs = userInfoItem.querySelectorAll('svg');
    if (uSvgs.length > 0) {
      var uSvg = uSvgs[0];
      uSvg.innerHTML = '<path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"></path><circle cx="12" cy="7" r="4"></circle>';
      uSvg.setAttribute('viewBox', '0 0 24 24');
      uSvg.setAttribute('fill', 'none');
      uSvg.setAttribute('stroke', 'currentColor');
      uSvg.setAttribute('stroke-width', '2');
      uSvg.setAttribute('stroke-linecap', 'round');
      uSvg.setAttribute('stroke-linejoin', 'round');
      for (var j = 1; j < uSvgs.length; j++) uSvgs[j].remove();
    }

    // Ubah text dari Logout menjadi User Info
    var uWalker = document.createTreeWalker(userInfoItem, NodeFilter.SHOW_TEXT, null, false);
    var uTextNode;
    while (uTextNode = uWalker.nextNode()) {
      if (uTextNode.nodeValue.trim() === 'Logout') uTextNode.nodeValue = 'User Info';
    }

    var uBadges = userInfoItem.querySelectorAll('[class*="badge"], [class*="indicator"], [class*="dot"], [class*="update"]');
    for (var k = 0; k < uBadges.length; k++) uBadges[k].remove();

    // Event listener untuk memanggil modal baru
    userInfoItem.addEventListener('click', function (e) {
      e.preventDefault(); e.stopPropagation(); openUserInfoModal();
    });

    // 3. Sisipkan (Insert) ke sidebar n8n
    // Urutan dari atas ke bawah: [JIT Access] -> User Info -> Logout
    logoutBtn.parentNode.insertBefore(userInfoItem, logoutBtn);
    if (requestItem) {
      logoutBtn.parentNode.insertBefore(requestItem, userInfoItem);
    }
  }

  // ── Initialization ───────────────────────────────────────────────────────

  // Poll until the logout button is available, then inject menu items
  var menuInterval = setInterval(function () {
    injectMenuItems();
    // Stop polling once injected (use userinfo as sentinel — always present)
    if (document.getElementById('knox-menu-userinfo')) {
      clearInterval(menuInterval);
    }
  }, 500);

  // Re-inject on SPA navigation (n8n uses client-side routing)
  var lastPath = window.location.pathname;
  setInterval(function () {
    if (window.location.pathname !== lastPath) {
      lastPath = window.location.pathname;
      // Update workflow ID if modal is open
      var wfInput = document.getElementById('knox-workflow-id');
      if (wfInput) {
        var newId = detectWorkflowID();
        if (newId) {
          wfInput.value = newId;
          wfInput.classList.add('knox-auto-filled');
        }
      }
    }
    // Re-inject if menu items were removed (n8n re-renders sidebar)
    if (!document.getElementById('knox-menu-userinfo')) {
      injectMenuItems();
    }
  }, 1000);

  // ── Watermark Overlay ───────────────────────────────────────────────────

  function initWatermark() {
    // Only render if globally enabled via nginx-injected config
    if (!window.__KNOX_WATERMARK__) return;
    if (document.getElementById('knox-watermark')) return;

    fetch('/knox-api/user-info')
      .then(function (resp) {
        if (!resp.ok) throw new Error('Failed');
        return resp.json();
      })
      .then(function (data) {
        var name = data.display_name || 'User';
        var email = data.email || '';
        var label = name + (email ? ' (' + email + ')' : '');
        renderWatermark(label);
      })
      .catch(function () {
        // Silently skip watermark on error
      });
  }

  function renderWatermark(label) {
    if (document.getElementById('knox-watermark')) return;

    var overlay = document.createElement('div');
    overlay.id = 'knox-watermark';
    overlay.className = 'knox-watermark-overlay';

    var inner = document.createElement('div');
    inner.className = 'knox-watermark-inner';

    // Fill with enough repeated text to cover the rotated area
    var count = 120;
    for (var i = 0; i < count; i++) {
      var span = document.createElement('span');
      span.className = 'knox-watermark-text';
      span.textContent = label;
      inner.appendChild(span);
    }

    overlay.appendChild(inner);
    document.body.appendChild(overlay);

    // Fade in
    requestAnimationFrame(function () {
      overlay.classList.add('knox-watermark-visible');
    });
  }

  // Initialize watermark after a short delay to ensure auth session is ready
  setTimeout(initWatermark, 1500);

})();
