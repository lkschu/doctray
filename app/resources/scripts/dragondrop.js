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
    dragLeaveHandler(ev)

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
        console.log(`Types: ${ev.dataTransfer.types}`)

        // var input = document.querySelector('input[type="file"]')

        var data = new FormData()
        for (let i = 0; i<items.length; i++) {
            if (items[i].kind === 'file') {
                const file = items[i].getAsFile();
                if (file) {
                    data.append('files', file)
                }
            } else {
                items[i].getAsString((str)=> { console.log(`nofile(${i}): ${str}`) })
            }

        }
        const text_input = document.getElementById('docUpload-text').value;
        data.append('title', text_input)
        // htmx.ajax('POST', '/doc-create', {target:"#doc-container", swap:'outerHTML'}, data)
        console.log(`source: ${ev.currentTarget}`)
        htmx.ajax('POST', '/tray/doc-create', {values: {files:data.getAll('files'),title:text_input}, source:ev.currentTarget, target:"#doc-container", swap:'outerHTML scroll:bottom'})

        //// const response = fetch('/doc-create', {
        ////     method: 'POST',
        ////     body: data
        //// })
        //// // response.then(response => response.text()).then(text => { console.log('Response as string:', text) }).catch(error => { console.error('Fetch error:', error) });
        //// response.then(response => response.text()).then(text => { htmx.swap("#doc-container", text, {swapStyle: 'outerHTML scroll:bottom'}) }).catch(error => { console.error('Fetch error:', error) });
        // window.location.reload() // Reload page after successfull upload ( because i am to stupid to copy htmx ajax behavior )
    }
    /// INFO: dataTransfer.files is deprecated
    ///else {
    ///    // Use DataTransfer interface to access the file(s)
    ///    [...ev.dataTransfer.files].forEach((file, i) => {
    ///        console.log(`… file[${i}].name = ${file.name}`);
    ///    });
    ///}
}

function dragLeaveHandler(ev) {
  var element = document.getElementById("drop_zone");
    element.classList.remove("markzone")
}
function dragOverHandler(ev) {
  console.log("File(s) in drop zone");
  var element = document.getElementById("drop_zone");
    element.classList.add("markzone")

  // Prevent default behavior (Prevent file from being opened)
  ev.preventDefault();
}
function get_file_names(){
    document.getElementById("docUpload").click()
}

function sub_uploadbutton_change(obj) {
    var file = obj.value;
    console.log('f:', file)
    var files = obj.files;
    console.log('ff:', files)

    var buttontext = ""
    if (!files) {
        return
    }

    if (files.length > 1) {
        buttontext = "(" + files.length + "):"
        for (var i = 0; i<files.length; i++) {
            buttontext = buttontext + " " + files[i].name + ";"
        }
    } else {
        buttontext = files[0].name
    }

    console.log('fff:', buttontext)
    // var fileName = file.split("\\");
    // document.getElementById("docUpload-label").innerHTML = fileName[fileName.length - 1];
    document.getElementById("docUpload-label").innerHTML = buttontext;

    // document.myForm.submit();
    event.preventDefault();
}


