// app.js — minimal JavaScript for the Listas application
// Uses SortableJS for drag-and-drop and fetch to persist the order

// Initialise SortableJS when the page loads
document.addEventListener('DOMContentLoaded', function() {
    initSortable();
    markActiveTab();
    initAutosaveForms();
});

// Also reinitialise after an HTMX swap (for dynamic pages)
document.addEventListener('htmx:afterSwap', function() {
    initSortable();
});

// markActiveTab highlights the matching item in the mobile bottom tab bar
function markActiveTab() {
    var path = window.location.pathname;
    document.querySelectorAll('[data-tab]').forEach(function(el) {
        var tab = el.getAttribute('data-tab');
        var active =
            (tab === 'home' && path === '/') ||
            (tab === 'lists' && path.indexOf('/my-lists') === 0) ||
            (tab === 'profile' && (path.indexOf('/settings') === 0 || path.indexOf('/password') === 0 || path.indexOf('/admin') === 0));
        if (active) {
            el.classList.remove('text-gray-400', 'dark:text-gray-500');
            el.classList.add('text-violet-600', 'dark:text-violet-400');
            var img = el.querySelector('img');
            if (img) img.classList.remove('grayscale', 'opacity-70');
        }
    });
}

// initSortable sets up drag-and-drop on the item list
function initSortable() {
    var list = document.getElementById('sortable-items');
    if (!list) return;

    // Prevent duplicate initialisation
    if (list.sortableInstance) return;

    list.sortableInstance = new Sortable(list, {
        animation: 200,           // 200ms smooth animation
        handle: '.drag-handle',   // drag only via the handle (⠿)
        ghostClass: 'sortable-ghost',
        chosenClass: 'sortable-chosen',
        // Update rank numbers after each drag
        onEnd: function() {
            updateRankNumbers();
            // On edit pages with data-autosave-order, persist the order automatically
            var autoOrderID = list.getAttribute('data-autosave-order');
            if (autoOrderID) {
                saveOrder(parseInt(autoOrderID, 10));
            }
        }
    });
}

// rankBadgeClass returns the CSS classes for a rank badge
function rankBadgeClass(pos, size) {
    var base = 'rank-number rounded-full flex items-center justify-center font-bold shrink-0 ';
    base += (size === 'sm') ? 'w-6 h-6 text-[10px] ' : 'w-7 h-7 text-[11px] ';
    if (pos === 1) return base + 'bg-gradient-to-br from-yellow-300 to-amber-500 text-amber-900 shadow-sm';
    if (pos === 2) return base + 'bg-gradient-to-br from-gray-200 to-gray-400 text-gray-600 shadow-sm';
    if (pos === 3) return base + 'bg-gradient-to-br from-orange-300 to-orange-500 text-white shadow-sm';
    return base + 'bg-gray-100/80 dark:bg-white/10 text-gray-400';
}

// updateRankNumbers refreshes the position numbers after reordering
function updateRankNumbers() {
    var items = document.querySelectorAll('#sortable-items .sortable-item');
    items.forEach(function(item, index) {
        var rankNum = item.querySelector('.rank-number');
        if (rankNum) {
            rankNum.textContent = index + 1;
            rankNum.className = rankBadgeClass(index + 1, 'lg');
        }
    });
}

// saveOrder sends the new item order to the server
function saveOrder(listID) {
    var items = document.querySelectorAll('#sortable-items .sortable-item');
    // Collect IDs in current order
    var ids = [];
    items.forEach(function(item) {
        ids.push(parseInt(item.getAttribute('data-id')));
    });

    // POST to server as JSON
    fetch('/lists/' + listID + '/reorder', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(ids)
    }).then(function(response) {
        if (response.ok) {
            // Show visual feedback
            var btn = document.querySelector('.btn-save-order');
            if (btn) {
                var originalText = btn.textContent;
                btn.textContent = 'Gardado!';
                btn.disabled = true;
                setTimeout(function() {
                    btn.textContent = originalText;
                    btn.disabled = false;
                }, 1500);
            }
        } else {
            alert('Erro ao gardar a orde');
        }
    }).catch(function() {
        alert('Erro de conexom');
    });
}

// --- Autosave ---------------------------------------------------------------
// Forms marked with data-autosave submit themselves automatically:
// text fields debounce 800ms, other changes (toggles, selects) save after 150ms.

function performAutosave(form) {
    var status = form.querySelector('[data-autosave-status]');
    if (status) status.textContent = 'A gardar…';
    fetch(form.action, {
        method: 'POST',
        body: new FormData(form),
        headers: { 'X-Autosave': '1' }
    }).then(function(resp) {
        if (status) status.textContent = resp.ok ? 'Gardado automaticamente' : 'Erro ao gardar';
    }).catch(function() {
        if (status) status.textContent = 'Erro de rede';
    });
}

// autosaveForm saves a form immediately (used by Alpine components after
// changing bound hidden inputs, where no native input event fires).
function autosaveForm(form) {
    if (!form) return;
    clearTimeout(form._autosaveTimer);
    performAutosave(form);
}

function initAutosaveForms() {
    document.querySelectorAll('form[data-autosave]').forEach(function(form) {
        form.addEventListener('input', function(e) {
            clearTimeout(form._autosaveTimer);
            form._autosaveTimer = setTimeout(function() { performAutosave(form); }, 800);
        });
        form.addEventListener('change', function(e) {
            if (e.target.type === 'file') return;
            clearTimeout(form._autosaveTimer);
            form._autosaveTimer = setTimeout(function() { performAutosave(form); }, 150);
        });
    });
}

// --- Toast ------------------------------------------------------------------

function showToast(message) {
    var existing = document.querySelector('.app-toast');
    if (existing) existing.remove();
    var toast = document.createElement('div');
    toast.className = 'app-toast';
    toast.textContent = message;
    document.body.appendChild(toast);
    requestAnimationFrame(function() { toast.classList.add('app-toast-visible'); });
    setTimeout(function() {
        toast.classList.remove('app-toast-visible');
        setTimeout(function() { toast.remove(); }, 300);
    }, 2500);
}
