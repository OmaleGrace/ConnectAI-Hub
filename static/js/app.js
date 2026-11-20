document.addEventListener("DOMContentLoaded", () => {
  const toggle=document.querySelector(".menu-toggle"), nav=document.querySelector(".nav-links");
  if(toggle&&nav) toggle.addEventListener("click",()=>{const open=nav.classList.toggle("open");toggle.setAttribute("aria-expanded",String(open));});
  const search=document.getElementById("moduleSearch"), cards=[...document.querySelectorAll("#moduleGrid [data-search]")];
  const emptySearch=document.getElementById("moduleSearchEmpty");
  if(search){
    const filterTools=()=>{const q=search.value.trim().toLocaleLowerCase();let shown=0;cards.forEach(card=>{const haystack=(card.dataset.search||card.textContent||"").toLocaleLowerCase();const match=!q||haystack.includes(q);card.hidden=!match;if(match)shown++;});if(emptySearch)emptySearch.hidden=shown!==0;};
    search.addEventListener("input",filterTools);search.addEventListener("search",filterTools);filterTools();
  }
  // First-visit guided tour, with a persistent manual replay button.
  const tour=document.getElementById("connectAITour");
  if(tour){
    const steps=[
      ["Your AI workspace, all in one place.","Your dashboard brings your seven AI workspaces together so you can get started quickly."],
      ["Find the right AI tool.","Use the dashboard search to find tools by name or by what they help you do."],
      ["Choose a workspace.","Open Study, Career, Business, Document, Website, Support or Data AI to start a focused conversation."],
      ["Keep track of your usage.","Your plan card shows today's AI requests, your allowance and how much remains."],
      ["Make the workspace yours.","Visit Account to change your theme, manage sessions and review your profile. You can replay this tour from the navigation anytime."]
    ];
    let index=0;const title=document.getElementById("tourTitle"),desc=document.getElementById("tourDescription"),count=document.getElementById("tourStepCount"),progress=document.getElementById("tourProgress"),back=document.getElementById("tourBack"),next=document.getElementById("tourNext");
    const close=()=>{tour.classList.remove("open");tour.setAttribute("aria-hidden","true");document.body.style.overflow="";try{localStorage.setItem("connectai-tour-v1","done")}catch(_){}};
    const render=()=>{title.textContent=steps[index][0];desc.textContent=steps[index][1];count.textContent=`STEP ${index+1} OF ${steps.length}`;progress.style.width=`${((index+1)/steps.length)*100}%`;back.disabled=index===0;next.textContent=index===steps.length-1?"Finish ✓":"Next →";};
    const open=()=>{index=0;render();tour.classList.add("open");tour.setAttribute("aria-hidden","false");document.body.style.overflow="hidden";next.focus();};
    document.querySelectorAll("[data-tour-start]").forEach(button=>button.addEventListener("click",open));document.getElementById("tourSkip")?.addEventListener("click",close);back?.addEventListener("click",()=>{if(index>0){index--;render();}});next?.addEventListener("click",()=>{if(index===steps.length-1){close();return;}index++;render();});
    tour.addEventListener("click",e=>{if(e.target===tour)close();});document.addEventListener("keydown",e=>{if(e.key==="Escape"&&tour.classList.contains("open"))close();});
    if(location.pathname==="/dashboard"){let done=false;try{done=localStorage.getItem("connectai-tour-v1")==="done"}catch(_){}if(!done)open();}
  }

  const form=document.getElementById("chatForm");
  if(form){
    const slug=form.dataset.module, csrf=form.dataset.csrf;
    const box=document.getElementById("chatMessages"), input=document.getElementById("chatInput");
    const button=document.getElementById("sendButton"), errorBox=document.getElementById("chatError");
    if(input.dataset.initialPrompt){input.value=input.dataset.initialPrompt;input.focus();}
    const empty=document.getElementById("chatEmpty");
    const escapeHTML=value=>value.replace(/[&<>"']/g,ch=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[ch]));
    const formatAnswer=value=>{let safe=escapeHTML(value);safe=safe.replace(/```([\w-]*)\n([\s\S]*?)```/g,(_,lang,code)=>`<pre><code>${code.replace(/\n$/,'')}</code></pre>`);safe=safe.replace(/`([^`\n]+)`/g,"<code>$1</code>").replace(/\*\*([^*\n]+)\*\*/g,"<strong>$1</strong>").replace(/\*([^*\n]+)\*/g,"<em>$1</em>");return safe.split(/\n{2,}/).map(part=>part.startsWith("<pre>")?part:`<p>${part.replace(/\n/g,"<br>")}</p>`).join("");};
    const regenerateButton=document.getElementById("regenerateLast");
    const refreshRegenerate=()=>{if(regenerateButton)regenerateButton.disabled=!box.querySelector(".chat-bubble.user");};
    const addMessage=(role,text,pending=false,messageId=0)=>{
      if(empty) empty.remove();
      const bubble=document.createElement("div");bubble.className="chat-bubble "+role+(pending?" pending":"");bubble.dataset.messageText=text;if(messageId)bubble.dataset.messageId=String(messageId);
      const label=document.createElement("small");label.textContent=role==="user"?"You":"ConnectAI assistant";
      const body=document.createElement("div");if(role==="model"&&!pending)body.innerHTML=formatAnswer(text);else body.textContent=text;
      bubble.append(label,body);
      if(role==="model"&&!pending){const actions=document.createElement("div");actions.className="message-actions";const copy=document.createElement("button");copy.type="button";copy.className="text-tool";copy.textContent="Copy";copy.addEventListener("click",async()=>{try{await navigator.clipboard.writeText(text);copy.textContent="Copied ✓";setTimeout(()=>copy.textContent="Copy",1400);}catch(_){errorBox.textContent="Clipboard access is unavailable.";errorBox.hidden=false;}});actions.append(copy);
        if(messageId){const feedback=document.createElement("button");feedback.type="button";feedback.className="text-tool";feedback.textContent="Rate response";feedback.addEventListener("click",async()=>{const raw=prompt("Rate this answer from 1 (poor) to 5 (excellent):","5");if(raw===null)return;const rating=Number(raw);if(!Number.isInteger(rating)||rating<1||rating>5){errorBox.textContent="Enter a number from 1 to 5.";errorBox.hidden=false;return;}const comment=prompt("Optional feedback (500 characters maximum):","")||"";try{const res=await fetch(`/api/modules/${encodeURIComponent(slug)}/feedback`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({csrf_token:csrf,message_id:messageId,rating,comment})});if(!res.ok)throw new Error(await res.text());feedback.textContent="Thanks for your feedback ✓";feedback.disabled=true;}catch(err){errorBox.textContent=err.message||"Unable to save feedback.";errorBox.hidden=false;}});actions.append(feedback);}
        bubble.append(actions);}
      box.appendChild(bubble);box.scrollTop=box.scrollHeight;return bubble;
    };
    const searchInput=document.getElementById("chatSearch");if(searchInput)searchInput.addEventListener("input",()=>{const q=searchInput.value.trim().toLocaleLowerCase();box.querySelectorAll(".chat-bubble").forEach(b=>b.hidden=!!q&&!(b.dataset.messageText||"").toLocaleLowerCase().includes(q));});
    const loadHistory=async()=>{
      try{
        const response=await fetch(`/api/modules/${encodeURIComponent(slug)}/messages`);
        if(!response.ok) return;
        const messages=await response.json();
        messages.forEach(m=>addMessage(m.role==="user"?"user":"model",m.text,false,m.id));refreshRegenerate();
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
      const transcript=bubbles.map(b=>{const label=b.querySelector("small")?.textContent||"Message";return `## ${label}\n\n${b.dataset.messageText||""}`;}).join("\n\n---\n\n");
      const blob=new Blob([`# ConnectAI Hub — ${slug} conversation\n\n${transcript}`],{type:"text/markdown;charset=utf-8"});
      const url=URL.createObjectURL(blob);const link=document.createElement("a");
      link.href=url;link.download=`connectai-${slug}-conversation.md`;link.click();URL.revokeObjectURL(url);
    });
    if(regenerateButton)regenerateButton.addEventListener("click",async()=>{if(regenerateButton.disabled)return;regenerateButton.disabled=true;regenerateButton.textContent="Regenerating…";errorBox.hidden=true;const old=[...box.querySelectorAll(".chat-bubble.model")];const previous=old[old.length-1];const pending=addMessage("model","Generating a fresh answer…",true);try{const response=await fetch(`/api/modules/${encodeURIComponent(slug)}/regenerate`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({csrf_token:csrf})});const text=await response.text();if(!response.ok)throw new Error(text||"Unable to regenerate this answer.");const data=JSON.parse(text);pending.remove();if(previous)previous.remove();addMessage("model",data.answer||"No response was returned.");}catch(err){pending.remove();errorBox.textContent=err.message||"Regeneration failed.";errorBox.hidden=false;}finally{regenerateButton.textContent="Regenerate answer";refreshRegenerate();}});
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


    const tempSlider=document.getElementById("workspaceTemperature"),tempValue=document.getElementById("temperatureValue");
    if(tempSlider&&tempValue)tempSlider.addEventListener("input",()=>tempValue.textContent=Number(tempSlider.value).toFixed(1));
    document.getElementById("saveWorkspacePreferences")?.addEventListener("click",async()=>{const status=document.getElementById("workspaceSaveStatus"),btn=document.getElementById("saveWorkspacePreferences");btn.disabled=true;status.textContent="Saving…";try{const res=await fetch(`/api/modules/${encodeURIComponent(slug)}/preferences`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({csrf_token:csrf,instructions:document.getElementById("workspaceInstructions").value,temperature:Number(tempSlider.value),pinned:document.getElementById("workspacePinned").checked})});if(!res.ok)throw new Error(await res.text());status.textContent="Preferences saved.";}catch(err){status.textContent=err.message||"Unable to save preferences.";}finally{btn.disabled=false;}});

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
        pending.remove();addMessage("model",data.answer||"No response was returned.",false,data.message_id);refreshRegenerate();
      }catch(err){
        pending.remove();errorBox.textContent=err.message||"Something went wrong. Please try again.";errorBox.hidden=false;
      }finally{
        button.disabled=false;button.textContent="Send message ↗";input.focus();
      }
    });
  }
});




// Device-local appearance preference. "system" follows the OS/browser preference.
(() => {
  const root = document.documentElement;
  const key = "connectai-theme";
  const select = document.getElementById("themePreference");
  let saved = "system";
  try { saved = localStorage.getItem(key) || "system"; } catch (_) {}
  root.dataset.theme = saved;
  if (select) {
    select.value = saved;
    select.addEventListener("change", () => {
      const value = ["light", "dark", "system"].includes(select.value) ? select.value : "system";
      root.dataset.theme = value;
      try { localStorage.setItem(key, value); } catch (_) {}
    });
  }
})();

// Progressive Web App installation. Never leave the button in a loading state.
let connectAIInstallPrompt = null;
const installButtons = () => document.querySelectorAll("[data-install-app]");
window.addEventListener("beforeinstallprompt", (event) => {
  event.preventDefault();
  connectAIInstallPrompt = event;
  installButtons().forEach((button) => { button.hidden = false; button.disabled = false; button.textContent = "Install app"; });
});
window.addEventListener("appinstalled", () => {
  connectAIInstallPrompt = null;
  installButtons().forEach((button) => { button.hidden = true; button.disabled = false; });
});
document.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-install-app]");
  if (!button) return;
  if (!connectAIInstallPrompt) {
    const ios = /iphone|ipad|ipod/i.test(navigator.userAgent);
    alert(ios ? "To add ConnectAI Hub, tap Share in Safari, then choose Add to Home Screen." : "If installation is available, open this site in Chrome or Edge and use the browser menu → Install ConnectAI Hub (or Add to Home screen). If the option is missing, check that the site is online and reload it.");
    return;
  }
  const promptEvent = connectAIInstallPrompt;
  connectAIInstallPrompt = null;
  button.disabled = true;
  button.textContent = "Opening install…";
  try {
    await promptEvent.prompt();
    const choice = await Promise.race([
      promptEvent.userChoice,
      new Promise((resolve) => setTimeout(() => resolve({outcome: "dismissed", timedOut: true}), 15000))
    ]);
    if (choice && choice.outcome === "accepted") {
      button.textContent = "Installing…";
      setTimeout(() => { button.disabled = false; button.textContent = "Install app"; }, 8000);
    } else {
      button.textContent = "Install app";
      button.disabled = false;
    }
  } catch (_) {
    button.textContent = "Install app";
    button.disabled = false;
    alert("The browser could not start installation. Reload the page and try again, or use the browser menu → Install app.");
  }
});
if ("serviceWorker" in navigator && location.protocol === "https:") {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/service-worker.js", {updateViaCache: "none"})
      .then((registration) => registration.update().catch(() => {}))
      .catch((error) => console.warn("ConnectAI service worker unavailable:", error));
  });
}

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
