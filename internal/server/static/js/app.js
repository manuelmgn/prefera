// app.js — minimal JavaScript for the Prefera application
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
            (tab === 'admin' && path.indexOf('/admin') === 0) ||
            (tab === 'profile' && (path.indexOf('/settings') === 0 || path.indexOf('/password') === 0));
        if (active) {
            el.classList.remove('text-gray-400', 'dark:text-gray-500');
            el.classList.add('text-violet-600', 'dark:text-violet-400');
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
            showToast('Erro ao gardar a orde');
        }
    }).catch(function() {
        showToast('Erro de rede. Tenta de novo.');
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
    // Acepta o formulario ou calquera elemento dentro del
    if (!form) return;
    if (form.tagName !== 'FORM') form = form.closest('form');
    if (!form || form.tagName !== 'FORM') return;
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

// --- Tooltip ----------------------------------------------------------------
// Reusable Alpine component: x-data="tooltip()".
// Trigger with @mouseenter="show()" @mouseleave="hide()" @click="toggle()",
// or @click="flash()" to show briefly (e.g. alongside another click action).
// The bubble is rendered next to the trigger inside a relative wrapper.

function tooltip() {
    return {
        open: false,
        _timer: null,
        show() {
            clearTimeout(this._timer);
            this.open = true;
        },
        hide() {
            clearTimeout(this._timer);
            this.open = false;
        },
        toggle() {
            if (this.open) { this.hide(); } else { this.show(); }
        },
        flash(ms) {
            this.show();
            var self = this;
            this._timer = setTimeout(function() { self.open = false; }, ms || 2500);
        }
    };
}

// --- Alpine components compartidos -------------------------------------------

// imageUploader handles the IMGBB upload for an item's image field.
// Usage: x-data="imageUploader(item)" inside an Alpine scope where `item`
// has an `image` property (e.g. an item in a x-for loop).
// imageLooksOk reports whether a URL looks like an image link
// (same rules as the server-side ValidateImageURL).
function imageLooksOk(v) {
    const lower = (v || '').trim().toLowerCase();
    return lower.includes('i.ibb.co/') || lower.includes('ibb.co/') ||
        /\.(jpg|jpeg|png|gif|webp|svg|bmp|avif)(\?|#|$)/.test(lower);
}

function imageUploader(item) {
    const maxSize = 10 * 1024 * 1024; // 10 MB, o mesmo límite ca no servidor
    return {
        uploading: false,
        uploadError: '',
        imageError: '',
        validateImageUrl() {
            const v = (item.image || '').trim();
            if (!v) { this.imageError = ''; return; }
            if (!/^https?:\/\//i.test(v)) {
                this.imageError = 'A URL da imaxe debe começar por http:// ou https://.';
                return;
            }
            const ok = imageLooksOk(v);
            this.imageError = ok ? '' : 'Esa URL nom parece dunha imaxe. Usa un enlace que remate en .jpg, .png, .gif ou .webp, ou sube a imaxe desde o teu dispositivo.';
        },
        onImageLoadError() {
            const v = (item.image || '').trim();
            if (!v) return; // src baleiro: o propio reset dispara error, ignorar
            item.image = '';
            this.imageError = imageLooksOk(v)
                ? 'Non se puido cargar a imaxe desde esa URL. Verifica que o enlace sexa correcto ou sube a imaxe desde o teu dispositivo.'
                : 'Esa URL nom parece dunha imaxe. Usa un enlace que remate en .jpg, .png, .gif ou .webp, ou sube a imaxe desde o teu dispositivo.';
        },
        async doUpload(event) {
            const file = event.target.files[0];
            if (!file) return;
            if (!file.type.startsWith('image/')) {
                this.uploadError = 'O ficheiro seleccionado nom é unha imaxe' +
                    (file.type ? ` (tipo «${file.type}»)` : '') +
                    '. Escolle un ficheiro JPG, PNG, GIF ou WebP.';
                event.target.value = '';
                return;
            }
            if (file.size > maxSize) {
                this.uploadError = `A imaxe é demasiado grande (${Math.round(file.size / 1048576)} MB; máx. 10 MB). Reduce o tamaño e tenta de novo.`;
                event.target.value = '';
                return;
            }
            this.uploading = true;
            this.uploadError = '';
            const fd = new FormData();
            fd.append('image', file);
            try {
                const r = await fetch('/api/upload-image', { method: 'POST', body: fd });
                const d = await r.json();
                if (r.ok && d.url) {
                    item.image = d.url;
                    this.imageError = '';
                } else {
                    this.uploadError = (d && d.error) || 'A imaxe nom se puido subir. Tenta de novo nuns segundos.';
                }
            } catch (e) {
                this.uploadError = 'Non foi possível contactar co servidor. Verifica a tua conexom e tenta de novo.';
            } finally {
                this.uploading = false;
                event.target.value = '';
            }
        }
    };
}

// linkValidator exposes a linkOk getter for an item's link field, checking
// against _allowedPatterns (defined on pages with item link fields).
// Usage: x-data="linkValidator(item)".
function linkValidator(item) {
    return {
        get linkOk() {
            return !item.link || _allowedPatterns.some(p => item.link.toLowerCase().includes(p.toLowerCase()));
        }
    };
}

// itemDetailsForm drives description/link/image editing for a single saved
// item (used on the list edit pages). Requires _allowedPatterns.
function itemDetailsForm(initialLink, initialImage) {
    const maxSize = 10 * 1024 * 1024; // 10 MB, o mesmo límite ca no servidor
    return {
        linkVal: initialLink,
        imageVal: initialImage,
        linkValid: true,
        imageError: '',
        uploading: false,
        uploadError: '',
        init() {
            this.validateLink();
            this.validateImage();
        },
        validateLink() {
            if (!this.linkVal) { this.linkValid = true; return; }
            const lower = this.linkVal.toLowerCase();
            this.linkValid = _allowedPatterns.some(p => lower.includes(p.toLowerCase()));
        },
        validateImage() {
            const v = (this.imageVal || '').trim();
            if (!v) { this.imageError = ''; return; }
            if (!/^https?:\/\//i.test(v)) {
                this.imageError = 'A URL da imaxe debe começar por http:// ou https://.';
                return;
            }
            const ok = imageLooksOk(v);
            this.imageError = ok ? '' : 'Esa URL nom parece dunha imaxe. Usa un enlace que remate en .jpg, .png, .gif ou .webp, ou sube a imaxe desde o teu dispositivo.';
        },
        onImageLoadError() {
            const v = (this.imageVal || '').trim();
            if (!v) return; // src baleiro: o propio reset dispara error, ignorar
            this.imageVal = '';
            this.imageError = imageLooksOk(v)
                ? 'Non se puido cargar a imaxe desde esa URL. Verifica que o enlace sexa correcto ou sube a imaxe desde o teu dispositivo.'
                : 'Esa URL nom parece dunha imaxe. Usa un enlace que remate en .jpg, .png, .gif ou .webp, ou sube a imaxe desde o teu dispositivo.';
        },
        async uploadImage(event) {
            const file = event.target.files[0];
            if (!file) return;
            if (!file.type.startsWith('image/')) {
                this.uploadError = 'O ficheiro seleccionado nom é unha imaxe' +
                    (file.type ? ` (tipo «${file.type}»)` : '') +
                    '. Escolle un ficheiro JPG, PNG, GIF ou WebP.';
                event.target.value = '';
                return;
            }
            if (file.size > maxSize) {
                this.uploadError = `A imaxe é demasiado grande (${Math.round(file.size / 1048576)} MB; máx. 10 MB). Reduce o tamaño e tenta de novo.`;
                event.target.value = '';
                return;
            }
            this.uploading = true;
            this.uploadError = '';
            const formData = new FormData();
            formData.append('image', file);
            try {
                const resp = await fetch('/api/upload-image', { method: 'POST', body: formData });
                const data = await resp.json();
                if (resp.ok && data.url) {
                    this.imageVal = data.url;
                    this.imageError = '';
                    autosaveForm(this.$el);
                } else {
                    this.uploadError = (data && data.error) || 'A imaxe nom se puido subir. Tenta de novo nuns segundos.';
                }
            } catch (e) {
                this.uploadError = 'Non foi possível contactar co servidor. Verifica a tua conexom e tenta de novo.';
            } finally {
                this.uploading = false;
                event.target.value = '';
            }
        }
    };
}
