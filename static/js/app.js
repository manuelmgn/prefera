// app.js — JavaScript mínimo para a aplicaçom Listas
// Usa SortableJS para drag & drop e fetch para gardar a orde

// Inicializar SortableJS quando a página carregar
document.addEventListener('DOMContentLoaded', function() {
    initSortable();
});

// Também reinicializar depois de uma troca HTMX (para páginas dinâmicas)
document.addEventListener('htmx:afterSwap', function() {
    initSortable();
});

// initSortable configura o drag & drop na lista de elementos
function initSortable() {
    var list = document.getElementById('sortable-items');
    if (!list) return;

    // Verificar se já foi inicializado (evitar duplicados)
    if (list.sortableInstance) return;

    list.sortableInstance = new Sortable(list, {
        animation: 200,           // Animaçom suave de 200ms
        handle: '.drag-handle',   // Só arrastar pelo handle (⠿)
        ghostClass: 'sortable-ghost',
        chosenClass: 'sortable-chosen',
        // Quando o utilizador termina de arrastar, actualizar os números
        onEnd: function() {
            updateRankNumbers();
        }
    });
}

// rankBadgeClass retorna as classes CSS para um badge de rank
function rankBadgeClass(pos, size) {
    var base = 'rank-number rounded-full flex items-center justify-center font-bold shrink-0 ';
    base += (size === 'sm') ? 'w-6 h-6 text-[10px] ' : 'w-7 h-7 text-[11px] ';
    if (pos === 1) return base + 'bg-gradient-to-br from-yellow-300 to-amber-500 text-amber-900 shadow-sm';
    if (pos === 2) return base + 'bg-gradient-to-br from-gray-200 to-gray-400 text-gray-600 shadow-sm';
    if (pos === 3) return base + 'bg-gradient-to-br from-orange-300 to-orange-500 text-white shadow-sm';
    return base + 'bg-gray-100/80 dark:bg-white/10 text-gray-400';
}

// updateRankNumbers actualiza os números de posiçom depois de reordenar
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

// saveOrder envia a nova orde dos elementos ao servidor
function saveOrder(listID) {
    var items = document.querySelectorAll('#sortable-items .sortable-item');
    // Recolher os IDs na ordem actual
    var ids = [];
    items.forEach(function(item) {
        ids.push(parseInt(item.getAttribute('data-id')));
    });

    // Enviar ao servidor via fetch (POST com JSON)
    fetch('/lists/' + listID + '/reorder', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(ids)
    }).then(function(response) {
        if (response.ok) {
            // Mostrar feedback visual
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
