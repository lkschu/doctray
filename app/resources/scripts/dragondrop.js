(() => {
    // Keep the existing client-side total limit, shared by picker/drop/paste.
    const maxAttachmentBytes = 10 * 1024 * 1024;

    // Input capabilities, not screen width: tablets need the same safeguards.
    // A hardware keyboard attached to a touch-first device still uses newline.
    const touchFirstInput = window.matchMedia("(hover: none), (pointer: coarse)");

    // Use the browser's timezone (including DST), never an offset or the server's zone.
    const localDate = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short" });
    const localDateWithYear = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short", year: "numeric" });
    const localClock = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
    const localFullDate = new Intl.DateTimeFormat(undefined, {
        weekday: "short", day: "2-digit", month: "short", year: "numeric",
        hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23", timeZoneName: "short"
    });
    function localizePostDates(root = document) {
        const selector = "time[data-date-format][datetime]";
        const dates = [...root.querySelectorAll(selector)];
        if (root.matches?.(selector)) dates.unshift(root);
        const currentYear = new Date().getFullYear();
        dates.forEach(element => {
            const at = new Date(element.dateTime);
            if (Number.isNaN(at.getTime())) return; // Leave malformed legacy values alone.
            const full = localFullDate.format(at);
            const compactDate = at.getFullYear() === currentYear ? localDate : localDateWithYear;
            element.textContent = element.dataset.dateFormat === "compact"
                ? `${compactDate.format(at)} · ${localClock.format(at)}` : full;
            element.title = full;
            element.setAttribute("aria-label", full);
        });
    }
    document.addEventListener("DOMContentLoaded", () => localizePostDates());
    document.addEventListener("htmx:load", event => localizePostDates(event.detail.elt));

    function focusComposer() {
        // Never summon a phone keyboard or focus a background tab on completion.
        if (touchFirstInput.matches || document.visibilityState !== "visible") return;
        const textarea = document.getElementById("docUpload-text");
        if (textarea?.isConnected) textarea.focus({ preventScroll: true });
    }

    // Mutable filter controls must not enter HTMX's queue: their workspace
    // response removes them. Only the persistent composer queues requests.
    let trayRequest = null;
    let trayFocusID = null;
    function updateWorkspaceControls() {
        document.querySelectorAll('#workspace-container [hx-target="#workspace-container"], #tags-edit-button, #tag-editor-form button:not(.tray-navigation-toggle), .doc-entry-button button, .doc-entry-tagview-segment, .doc-entry-undo').forEach(control => {
            control.disabled = !!trayRequest;
        });
    }
    document.addEventListener("htmx:beforeRequest", event => {
        if (event.defaultPrevented || !event.target.getAttribute("hx-sync")?.startsWith("#tray-container:")) return;
        // Disabling a native button can blur it before HTMX preserves focus.
        trayFocusID = event.target === document.activeElement && event.target.matches('.doc-entry-button-fav button, .doc-entry-tagview-segment, .tag-filter-chip, .tag-star-filter, #clear-filters-button, #tag-add-button')
            ? event.target.id : null;
        trayRequest = event.detail.xhr;
        updateWorkspaceControls();
    });
    document.addEventListener("htmx:afterRequest", event => {
        if (event.detail.xhr !== trayRequest) return;
        const focusID = trayFocusID;
        trayFocusID = null;
        trayRequest = null;
        updateWorkspaceControls();
        if (focusID && document.visibilityState === "visible" &&
            (document.activeElement === document.body || document.activeElement?.id === focusID)) {
            document.getElementById(focusID)?.focus({ preventScroll: true });
        }
    });
    // Do not return to an old button after the user clicks or tabs elsewhere.
    document.addEventListener("pointerdown", () => { trayFocusID = null; });
    document.addEventListener("keydown", event => {
        if (event.key === "Tab" || event.key === "Escape") trayFocusID = null;
    });
    document.addEventListener("focusin", event => {
        // Ignore the body's automatic focus after disabling the initiating button.
        if (event.target !== document.body && event.target.id !== trayFocusID) trayFocusID = null;
    });
    document.addEventListener("htmx:load", updateWorkspaceControls);

    // Browser-only disclosure state; desktop always shows the full controls.
    const narrowLayout = window.matchMedia("(max-width: 48rem)");
    let filtersOpen = false;
    function updateFilterDisclosure() {
        const toggle = document.getElementById("tag-filter-disclosure");
        if (!toggle) return; // Editing has its own Save/Cancel flow.
        document.getElementById("tag-container").dataset.filtersOpen = String(filtersOpen);
        toggle.setAttribute("aria-expanded", String(filtersOpen));
    }
    document.addEventListener("DOMContentLoaded", updateFilterDisclosure);
    document.addEventListener("htmx:load", updateFilterDisclosure);
    function initializeTrayNavigation() {
        const menu = document.getElementById("tray-navigation");
        if (!menu || typeof menu.showPopover !== "function") return;
        document.body.classList.add("tray-navigation-ready");
        document.addEventListener("click", event => {
            const button = event.target.closest(".tray-navigation-toggle");
            // Native dismissal should return to the invoker, not a phone textarea.
            if (button && !button.disabled) button.focus({ preventScroll: true });
        });
        narrowLayout.addEventListener("change", event => {
            if (!event.matches && menu.matches(":popover-open")) menu.hidePopover();
        });
    }
    document.addEventListener("DOMContentLoaded", initializeTrayNavigation);

    function tagScrollPanel(panel) {
        return narrowLayout.matches && panel.dataset.tagMode === "filter"
            ? panel.querySelector(".tag-filter-panel") : panel;
    }

    // Preserve only same-mode panel refreshes, not opening/closing the editor.
    const tagPanelScroll = new WeakMap();
    document.addEventListener("htmx:beforeSwap", event => {
        if (!event.detail.shouldSwap) return;
        const path = event.detail.requestConfig?.path;
        const targetID = event.detail.target?.id;
        const filterRefresh = targetID === "workspace-container" &&
            ["/tray/tag-toggle-filter", "/tray/star-filter", "/tray/filters-clear"].includes(path);
        const editorRefresh = targetID === "tag-container" && path === "/tray/tag-create";
        if (!filterRefresh && !editorRefresh) return;
        const panel = document.getElementById("tag-container");
        if (panel) tagPanelScroll.set(event.detail.xhr, { mode: panel.dataset.tagMode, top: tagScrollPanel(panel).scrollTop });
    });
    document.addEventListener("htmx:afterSwap", event => {
        // Expand before restoring scroll to the new inner panel.
        updateFilterDisclosure();
        const scroll = tagPanelScroll.get(event.detail.xhr);
        if (!scroll) return;
        tagPanelScroll.delete(event.detail.xhr);
        const panel = document.getElementById("tag-container");
        if (panel?.dataset.tagMode === scroll.mode) tagScrollPanel(panel).scrollTop = scroll.top;
    });

    document.addEventListener("click", event => {
        const toggle = event.target.closest("#tag-filter-disclosure");
        const collapse = event.target.closest("#tag-filter-collapse");
        if (toggle || collapse) {
            const focusWasOnToggle = toggle === document.activeElement;
            filtersOpen = toggle ? !filtersOpen : false;
            updateFilterDisclosure();
            if (collapse) document.getElementById("tag-filter-disclosure").focus({ preventScroll: true });
            else if (focusWasOnToggle && narrowLayout.matches) {
                // The summary is replaced by the expanded controls; keep keyboard focus local.
                document.querySelector("#tag-filter-content button:not(:disabled)")?.focus({ preventScroll: true });
            }
            return;
        }
        const remove = event.target.closest(".tag-editor-remove");
        if (remove && !remove.disabled) remove.closest(".tag-editor-row").remove();
    });

    // Failed Undo stays local to its Removed row; transient failures can retry.
    document.addEventListener("htmx:afterRequest", event => {
        const request = event.detail.requestConfig;
        if (request?.path !== "/tray/doc-restore" || event.detail.successful) return;
        const button = event.target.closest(".doc-entry-undo");
        if (!button) return;
        const message = button.closest(".doc-entry").querySelector(".doc-entry-undo-status");
        const status = event.detail.xhr.status;
        if (status === 404 || status === 410) {
            button.remove();
            message.textContent = "Cannot restore: the message or an attachment was already removed.";
        } else {
            message.textContent = "Could not restore the message. Try again.";
        }
        message.hidden = false;
    });

    function formatFileSize(bytes) {
        if (bytes < 1024) return `${bytes} B`;
        const unit = bytes < 1024 * 1024 ? "KiB" : "MiB";
        const divisor = unit === "KiB" ? 1024 : 1024 * 1024;
        return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(bytes / divisor)} ${unit}`;
    }

    function attachmentIcon(file) {
        const type = file.type.toLowerCase();
        const extension = file.name.includes(".") ? file.name.split(".").pop().toLowerCase() : "";
        if (type === "application/pdf" || extension === "pdf") return "picture_as_pdf";
        if (type.startsWith("image/") || /^(png|jpe?g|gif|webp|bmp|tiff?|svg|avif|hei[cf])$/.test(extension)) return "imagesmode";
        if (type.startsWith("audio/") || /^(mp3|wav|flac|ogg|m4a|aac|mka)$/.test(extension)) return "music_note";
        if (type.startsWith("video/") || /^(mp4|mkv|webm|mov|avi)$/.test(extension)) return "movie";
        if (type.startsWith("text/") || /^(txt|md|csv|json|ya?ml|xml|log)$/.test(extension)) return "description";
        if (/^(zip|tar|gz|tgz|7z|rar)$/.test(extension)) return "folder_zip";
        return "draft";
    }

    function resizeTextarea(textarea) {
        textarea.style.height = "auto";
        const style = getComputedStyle(textarea);
        const borders = parseFloat(style.borderTopWidth) + parseFloat(style.borderBottomWidth);
        textarea.style.height = `${textarea.scrollHeight + borders}px`;
        textarea.style.overflowY = textarea.scrollHeight > textarea.clientHeight ? "auto" : "hidden";
    }

    function initializeComposer() {
        const form = document.getElementById("form");
        if (!form || form.dataset.composerReady) return;
        form.dataset.composerReady = "true";

        const textarea = form.querySelector("#docUpload-text");
        const fileInput = form.querySelector("#docUpload");
        const dropZone = form.querySelector("#drop_zone");
        const attachments = form.querySelector("#docUpload-attachments");
        const attachmentSummary = form.querySelector("#docUpload-attachment-summary");
        const error = form.querySelector("#docUpload-error");
        const progress = form.querySelector("#progress");
        const sendButton = form.querySelector("#upload-button");
        const sendIcon = sendButton.querySelector(".material-symbols-outlined");
        let files = [];
        let submitting = false;
        let dragDepth = 0;

        function updateSendState() {
            sendButton.disabled = submitting || (!textarea.value.trim() && !files.length);
            sendButton.classList.toggle("is-sending", submitting);
            sendIcon.textContent = submitting ? "progress_activity" : "send";
            const label = submitting ? "Sending message…" : "Send message";
            sendButton.setAttribute("aria-label", label);
            sendButton.title = label;
            form.setAttribute("aria-busy", String(submitting));
        }

        function showError(message) {
            error.textContent = message;
            error.hidden = !message;
        }

        function updateAttachments() {
            const transfer = new DataTransfer();
            files.forEach(file => transfer.items.add(file));
            fileInput.files = transfer.files;
            attachments.replaceChildren();
            files.forEach((file, index) => {
                const item = document.createElement("li");
                const icon = document.createElement("span");
                icon.className = "material-symbols-outlined composer-attachment-icon";
                icon.textContent = attachmentIcon(file);
                icon.setAttribute("aria-hidden", "true");
                const name = document.createElement("span");
                name.className = "composer-attachment-name";
                name.textContent = file.name;
                name.title = file.name;
                const size = document.createElement("span");
                size.className = "composer-attachment-size";
                size.textContent = formatFileSize(file.size);
                const remove = document.createElement("button");
                remove.type = "button";
                remove.textContent = "Remove";
                remove.setAttribute("aria-label", `Remove ${file.name}`);
                remove.addEventListener("click", () => {
                    if (submitting) return;
                    files.splice(index, 1);
                    updateAttachments();
                    showError("");
                });
                item.append(icon, name, size, remove);
                attachments.append(item);
            });
            attachmentSummary.hidden = !files.length;
            const totalBytes = files.reduce((total, file) => total + file.size, 0);
            attachmentSummary.textContent = files.length
                ? `${files.length} attachment${files.length === 1 ? "" : "s"} · ${formatFileSize(totalBytes)}`
                : "";
            updateSendState();
        }

        function addFiles(incoming) {
            if (submitting) return;
            const combined = [...files, ...incoming];
            if (combined.reduce((total, file) => total + file.size, 0) > maxAttachmentBytes) {
                showError("Attachments must total 10 MiB or less.");
                // The picker replaced its FileList; restore the accepted draft.
                updateAttachments();
                return;
            }
            files = combined;
            updateAttachments();
            attachments.scrollTop = attachments.scrollHeight;
            showError("");
        }

        form.querySelector("#docUpload-label").addEventListener("click", () => fileInput.click());
        fileInput.addEventListener("change", () => addFiles(Array.from(fileInput.files)));
        textarea.addEventListener("input", () => {
            resizeTextarea(textarea);
            updateSendState();
        });
        textarea.addEventListener("keydown", event => {
            if (!touchFirstInput.matches && event.key === "Enter" && !event.shiftKey && !event.isComposing && event.keyCode !== 229) {
                event.preventDefault();
                if (!submitting) form.requestSubmit();
            }
        });
        textarea.addEventListener("paste", event => {
            const images = Array.from(event.clipboardData?.items || [])
                .filter(item => item.kind === "file" && item.type.startsWith("image/"))
                .map(item => item.getAsFile())
                .filter(Boolean);
            if (!images.length) return; // Leave ordinary text paste to the browser.
            event.preventDefault();
            if (submitting) return;
            addFiles(images);
            const text = event.clipboardData.getData("text/plain");
            if (text) textarea.setRangeText(text, textarea.selectionStart, textarea.selectionEnd, "end");
            resizeTextarea(textarea);
            updateSendState();
        });

        function isFileDrag(event) {
            return Array.from(event.dataTransfer?.types || []).includes("Files");
        }
        dropZone.addEventListener("dragenter", event => {
            if (!isFileDrag(event)) return;
            event.preventDefault();
            dragDepth += 1;
            dropZone.classList.add("markzone");
        });
        dropZone.addEventListener("dragover", event => {
            if (!isFileDrag(event)) return;
            event.preventDefault();
            event.dataTransfer.dropEffect = submitting ? "none" : "copy";
        });
        dropZone.addEventListener("dragleave", () => {
            dragDepth = Math.max(0, dragDepth - 1);
            if (!dragDepth) dropZone.classList.remove("markzone");
        });
        dropZone.addEventListener("drop", event => {
            dragDepth = 0;
            dropZone.classList.remove("markzone");
            const incoming = Array.from(event.dataTransfer?.files || []);
            if (!incoming.length) return; // Preserve ordinary text/link drops.
            event.preventDefault();
            addFiles(incoming);
        });

        form.addEventListener("htmx:beforeRequest", event => {
            if (submitting || (!textarea.value.trim() && !files.length)) {
                event.preventDefault();
                if (!submitting) showError("Write a message or attach a file first.");
                return;
            }
            showError("");
            submitting = true;
            form.querySelectorAll("button, textarea, input").forEach(control => { control.disabled = true; });
            updateSendState();
            progress.value = 0;
            progress.hidden = false;
        });
        form.addEventListener("htmx:xhr:progress", event => {
            if (event.detail.total > 0) progress.value = event.detail.loaded / event.detail.total * 100;
        });
        form.addEventListener("htmx:afterRequest", event => {
            submitting = false;
            form.querySelectorAll("button, textarea, input").forEach(control => { control.disabled = false; });
            progress.hidden = true;
            if (event.detail.successful) {
                // Only the message list is swapped; explicitly clear the sent draft.
                form.reset();
                files = [];
                updateAttachments();
                resizeTextarea(textarea);
                showError("");
            } else {
                showError("Could not send the message. Your draft is still here; try again.");
                if (form.isConnected) focusComposer();
            }
            updateSendState();
        });
        resizeTextarea(textarea);
        updateSendState();
        // Replace unconditional HTML autofocus without displacing another field.
        if (document.activeElement === document.body) focusComposer();
    }

    document.addEventListener("DOMContentLoaded", initializeComposer);
    document.addEventListener("htmx:load", initializeComposer);
    document.addEventListener("htmx:afterSettle", event => {
        const request = event.detail.requestConfig;
        if (request?.path !== "/tray/doc-create" || request.verb !== "post" || !event.detail.successful) return;
        // Desktop keeps its focus flow; touch-first devices never reopen a keyboard.
        focusComposer();
    });
    window.addEventListener("resize", () => {
        const textarea = document.getElementById("docUpload-text");
        if (textarea) resizeTextarea(textarea);
    });
    window.addEventListener("load", () => {
        const container = document.getElementById("doc-container");
        if (container) container.scrollTop = container.scrollHeight;
    });

    let lastActive = Date.now();
    document.addEventListener("visibilitychange", () => {
        if (document.visibilityState === "hidden") {
            lastActive = Date.now();
        } else if (Date.now() - lastActive > 15 * 60 * 1000) {
            const form = document.getElementById("form");
            const hasDraft = form && (form.querySelector("#docUpload-text").value.length ||
                form.querySelector("#docUpload").files.length || form.getAttribute("aria-busy") === "true");
            if (!hasDraft) location.reload();
        }
    });
})();
