(() => {
    // Keep the existing client-side total limit, shared by picker/drop/paste.
    const maxAttachmentBytes = 10 * 1024 * 1024;

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
                const name = document.createElement("span");
                name.textContent = file.name;
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
                item.append(name, remove);
                attachments.append(item);
            });
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
            updateSendState();
            progress.hidden = true;
            // A successful swap creates a fresh composer; failed requests keep this draft.
            if (!event.detail.successful) {
                showError("Could not send the message. Your draft is still here; try again.");
                if (form.isConnected) textarea.focus({ preventScroll: true });
            }
        });
        resizeTextarea(textarea);
        updateSendState();
    }

    document.addEventListener("DOMContentLoaded", initializeComposer);
    document.addEventListener("htmx:load", initializeComposer);
    document.addEventListener("htmx:afterSettle", event => {
        const request = event.detail.requestConfig;
        if (request?.path !== "/tray/doc-create" || request.verb !== "post" || !event.detail.successful) return;
        // Wait for the successful replacement to settle, then focus the new input.
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
            location.reload();
        }
    });
})();
