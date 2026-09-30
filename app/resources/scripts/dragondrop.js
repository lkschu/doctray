(() => {
    // Keep the existing client-side total limit, shared by picker/drop/paste.
    const maxAttachmentBytes = 10 * 1024 * 1024;

    // Mutable filter controls must not enter HTMX's queue: their workspace
    // response removes them. Only the persistent composer queues requests.
    let trayRequest = null;
    function updateWorkspaceControls() {
        document.querySelectorAll('#workspace-container [hx-target="#workspace-container"], #tag-editor-form button').forEach(control => {
            control.disabled = !!trayRequest;
        });
    }
    document.addEventListener("htmx:beforeRequest", event => {
        if (event.defaultPrevented || !event.target.getAttribute("hx-sync")?.startsWith("#tray-container:")) return;
        trayRequest = event.detail.xhr;
        updateWorkspaceControls();
    });
    document.addEventListener("htmx:afterRequest", event => {
        if (event.detail.xhr !== trayRequest) return;
        trayRequest = null;
        updateWorkspaceControls();
    });
    document.addEventListener("htmx:load", updateWorkspaceControls);
    document.addEventListener("click", event => {
        const remove = event.target.closest(".tag-editor-remove");
        if (remove && !remove.disabled) remove.closest(".tag-editor-row").remove();
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
            if (event.key === "Enter" && !event.shiftKey && !event.isComposing && event.keyCode !== 229) {
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
                if (form.isConnected) textarea.focus({ preventScroll: true });
            }
            updateSendState();
        });
        resizeTextarea(textarea);
        updateSendState();
    }

    document.addEventListener("DOMContentLoaded", initializeComposer);
    document.addEventListener("htmx:load", initializeComposer);
    document.addEventListener("htmx:afterSettle", event => {
        const request = event.detail.requestConfig;
        if (request?.path !== "/tray/doc-create" || request.verb !== "post" || !event.detail.successful) return;
        // Wait for the message list to settle, then return focus to the composer.
        const textarea = document.getElementById("docUpload-text");
        if (textarea) textarea.focus({ preventScroll: true });
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
