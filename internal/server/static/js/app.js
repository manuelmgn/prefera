// app.js — minimal JavaScript for the Listas application
// Uses SortableJS for drag-and-drop and fetch to persist the order

// Initialise SortableJS when the page loads
document.addEventListener('DOMContentLoaded', function() {
    initSortable();
});

// Also reinitialise after an HTMX swap (for dynamic pages)
document.addEventListener('htmx:afterSwap', function() {
    initSortable();
});

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
