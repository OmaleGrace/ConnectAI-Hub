document.addEventListener("DOMContentLoaded", () => {
  const toggle=document.querySelector(".menu-toggle"), nav=document.querySelector(".nav-links");
  if(toggle&&nav) toggle.addEventListener("click",()=>{const open=nav.classList.toggle("open");toggle.setAttribute("aria-expanded",String(open));});
  const search=document.getElementById("moduleSearch"), cards=document.querySelectorAll("#moduleGrid [data-search]");
  if(search) search.addEventListener("input",()=>{const q=search.value.trim().toLowerCase();cards.forEach(card=>card.hidden=!card.dataset.search.toLowerCase().includes(q));});

  const form=document.getElementById("chatForm");
  if(form){
    const slug=form.dataset.module, csrf=form.dataset.csrf;
    const box=document.getElementById("chatMessages"), input=document.getElementById("chatInput");
    const button=document.getElementById("sendButton"), errorBox=document.getElementById("chatError");
    const empty=document.getElementById("chatEmpty");
    const addMessage=(role,text,pending=false)=>{
      if(empty) empty.remove();
      const bubble=document.createElement("div");
      bubble.className="chat-bubble "+role+(pending?" pending":"");
      const label=document.createElement("small");
      label.textContent=role==="user"?"You":"ConnectAI assistant";
      const body=document.createElement("div");
      body.textContent=text;
      bubble.append(label,body);box.appendChild(bubble);box.scrollTop=box.scrollHeight;
      return bubble;
    };
    const loadHistory=async()=>{
      try{
        const response=await fetch(`/api/modules/${encodeURIComponent(slug)}/messages`);
        if(!response.ok) return;
        const messages=await response.json();
        messages.forEach(m=>addMessage(m.role==="user"?"user":"model",m.text));
      }catch(_){}
    };
    loadHistory();
    document.querySelectorAll(".quick-prompt").forEach(promptButton=>{
      promptButton.addEventListener("click",()=>{
        input.value=promptButton.dataset.prompt||"";
        input.focus();
        form.requestSubmit();
      });
    });
    const exportButton=document.getElementById("exportChat");
    if(exportButton) exportButton.addEventListener("click",()=>{
      const bubbles=[...box.querySelectorAll(".chat-bubble")];
      if(!bubbles.length){errorBox.textContent="There is no conversation to export yet.";errorBox.hidden=false;return;}
      const transcript=bubbles.map(b=>{
        const label=b.querySelector("small")?.textContent||"Message";
        const body=b.querySelector("div")?.textContent||"";
        return `${label}\n${body}`;
      }).join("\n\n");
      const blob=new Blob([transcript],{type:"text/plain;charset=utf-8"});
      const url=URL.createObjectURL(blob);const link=document.createElement("a");
      link.href=url;link.download=`connectai-${slug}-conversation.txt`;link.click();URL.revokeObjectURL(url);
    });
    const clearButton=document.getElementById("clearChat");
    if(clearButton) clearButton.addEventListener("click",async()=>{
      if(!confirm("Clear this module's saved conversation? This cannot be undone."))return;
      clearButton.disabled=true;
      try{
        const response=await fetch(`/api/modules/${encodeURIComponent(slug)}/messages`,{
          method:"DELETE",headers:{"Content-Type":"application/json"},
          body:JSON.stringify({csrf_token:csrf})
        });
        if(!response.ok) throw new Error(await response.text()||"Unable to clear conversation.");
        box.innerHTML=`<div class="chat-empty" id="chatEmpty"><span>✦</span><b>Your conversation starts here.</b><p>Ask a question or describe what you'd like help with.</p></div>`;
        errorBox.hidden=true;
      }catch(err){errorBox.textContent=err.message||"Unable to clear conversation.";errorBox.hidden=false;}
      finally{clearButton.disabled=false;}
    });

    const uploadForm=document.getElementById("documentUpload");
    if(uploadForm){
      const uploadButton=document.getElementById("analyzeButton");
      const fileInput=document.getElementById("documentFile");
      const questionInput=document.getElementById("documentQuestion");
      uploadForm.addEventListener("submit",async event=>{
        event.preventDefault();
        if(!fileInput.files.length||uploadButton.disabled)return;
        errorBox.hidden=true;
        const selected=fileInput.files[0];
        addMessage("user",`Uploaded file: ${selected.name}\nRequest: ${questionInput.value.trim()||"Please analyse this file."}`);
        uploadButton.disabled=true;uploadButton.textContent="Analysing…";
        const pending=addMessage("model","Reading and analysing your file…",true);
        const payload=new FormData();
        payload.append("document",selected);
        payload.append("question",questionInput.value.trim());
        payload.append("csrf_token",csrf);
        try{
          const response=await fetch(`/api/modules/${encodeURIComponent(slug)}/analyze`,{method:"POST",body:payload});
          const text=await response.text();
          if(!response.ok)throw new Error(text||"The file could not be analysed.");
          let data;try{data=JSON.parse(text)}catch(_){throw new Error("The server returned an invalid response.")};
          pending.remove();addMessage("model",data.answer||"No analysis was returned.");
          uploadForm.reset();
        }catch(err){
          pending.remove();errorBox.textContent=err.message||"File analysis failed. Please try again.";errorBox.hidden=false;
        }finally{
          uploadButton.disabled=false;uploadButton.textContent="Analyse file ↗";
        }
      });
    }

    form.addEventListener("submit",async event=>{
      event.preventDefault();
      const message=input.value.trim();
      if(!message||button.disabled)return;
      errorBox.hidden=true;
      addMessage("user",message);
      input.value="";button.disabled=true;button.textContent="Thinking…";
      const pending=addMessage("model","Generating a response…",true);
      try{
        const response=await fetch(`/api/modules/${encodeURIComponent(slug)}/chat`,{
          method:"POST",headers:{"Content-Type":"application/json"},
          body:JSON.stringify({message,csrf_token:csrf})
        });
        const text=await response.text();
        if(!response.ok) throw new Error(text||"The request could not be completed.");
        let data;try{data=JSON.parse(text)}catch(_){throw new Error("The server returned an invalid response.")}
        pending.remove();addMessage("model",data.answer||"No response was returned.");
      }catch(err){
        pending.remove();errorBox.textContent=err.message||"Something went wrong. Please try again.";errorBox.hidden=false;
      }finally{
        button.disabled=false;button.textContent="Send message ↗";input.focus();
      }
    });
  }
});


// Authentication helpers: password visibility and live input feedback.
document.addEventListener('click', (event) => {
  const button = event.target.closest('.toggle-password');
  if (!button) return;
  const input = button.parentElement.querySelector('input');
  if (!input) return;
  const showing = input.type === 'password';
  input.type = showing ? 'text' : 'password';
  button.textContent = showing ? 'Hide' : 'Show';
  button.setAttribute('aria-label', showing ? 'Hide password' : 'Show password');
});

document.querySelectorAll('[data-live-form]').forEach((form) => {
  const email = form.querySelector('[data-email-check]');
  const emailFeedback = form.querySelector('[data-email-feedback]');
  if (email && emailFeedback) {
    const checkEmail = () => {
      if (!email.value) { emailFeedback.textContent = ''; emailFeedback.className = 'live-feedback'; return; }
      const valid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.value);
      emailFeedback.textContent = valid ? 'Email format looks valid.' : 'Enter a valid email address.';
      emailFeedback.className = 'live-feedback ' + (valid ? 'valid' : 'invalid');
    };
    email.addEventListener('input', checkEmail); checkEmail();
  }
  const name = form.querySelector('[data-name-check]');
  const nameFeedback = form.querySelector('[data-name-feedback]');
  if (name && nameFeedback) name.addEventListener('input', () => {
    const ok = [...name.value.trim()].length >= 3;
    nameFeedback.textContent = ok ? 'Name length looks good.' : 'Enter at least 3 characters.';
    nameFeedback.className = 'live-feedback ' + (ok ? 'valid' : 'invalid');
  });
  const password = form.querySelector('input[name="password"], input[name="new_password"]');
  const feedback = form.querySelector('[data-password-feedback]');
  if (password && feedback) password.addEventListener('input', () => {
    const value = password.value;
    const checks = [value.length >= 8, /[A-Za-z]/.test(value), /[0-9]/.test(value), /[^A-Za-z0-9]/.test(value)];
    feedback.textContent = checks.map((ok, i) => (ok ? '✓ ' : '• ') + ['8+ characters', 'a letter', 'a number', 'a symbol'][i]).join('  ');
    feedback.className = 'live-feedback password-rules ' + (checks.every(Boolean) ? 'valid' : 'invalid');
  });
});
