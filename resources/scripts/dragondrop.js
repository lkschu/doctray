// const dropzone = document.getElementById('dropzone');

// ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
// 	dropzone.addEventListener(eventName, preventDefaults, false);
// });

// function preventDefaults (evt) {
// 	evt.preventDefault();
// 	evt.stopPropagation();
// }

// ['dragenter', 'dragover'].forEach(eventName => {
// 	dropzone.addEventListener(eventName, markzone, false);
// });

// ['dragleave', 'drop'].forEach(eventName => {
// 	dropzone.addEventListener(eventName, unmarkzone, false);
// });


// function markzone(evt) {
// 	dropzone.classList.add('markzone');
// }

// function unmarkzone(evt) {
// 	dropzone.classList.remove('markzone');
// }

// dropzone.addEventListener('drop', function (evt) {
// 	let dt = evt.dataTransfer;
// 	console.log ("dt", dt);
// 	let files = dt.files;
// 	handleFiles(files);
// });

// function handleFiles(files) {
// 	([...files]).forEach(uploadFile);
// 	([...files]).forEach(previewFile);
// }
//





/// src: https://developer.mozilla.org/en-US/docs/Web/API/HTML_Drag_and_Drop_API/File_drag_and_drop
function dropHandler(ev) {
    console.log("File(s) dropped");

    // Prevent default behavior (Prevent file from being opened)
    ev.preventDefault();

    if (ev.dataTransfer.items) {
        // Use DataTransferItemList interface to access the file(s)
        [...ev.dataTransfer.items].forEach((item, i) => {
            // If dropped items aren't files, reject them
            if (item.kind === "file") {
                const file = item.getAsFile();
                console.log(`… file[${i}].name = ${file.name}`);
            }
        });

        const items = ev.dataTransfer.items;
        var files = [];
        for (let i = 0; i<items.length; i++){
            if (items[i].kind === 'file') {
                const file = items[i].getAsFile();
                if (file) {
                    files.push(file);
                }
            }
        }
        console.log(`files(${files.length}): ${files}`)

        // var input = document.querySelector('input[type="file"]')
        // document.getElementById('docUpload').value = input.files
        //


        var data = new FormData()
        for (let i = 0; i<items.length; i++) {
            if (items[i].kind === 'file') {
                const file = items[i].getAsFile();
                if (file) {
                    data.append('files', file)
                }
            }
        }

        // data.append('files', files)
        // data.append('user', 'test')
        fetch('/upload', {
            method: 'POST',
            // headers: { "Content-Type":"multipart/form-data" },
            body: data
        })
    }
    /// INFO: dataTransfer.files is deprecated
    ///else {
    ///    // Use DataTransfer interface to access the file(s)
    ///    [...ev.dataTransfer.files].forEach((file, i) => {
    ///        console.log(`… file[${i}].name = ${file.name}`);
    ///    });
    ///}
}

function dragOverHandler(ev) {
  console.log("File(s) in drop zone");

  // Prevent default behavior (Prevent file from being opened)
  ev.preventDefault();
}

