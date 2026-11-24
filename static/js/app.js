/* ConnectAI Hub — shared helpers ------------------------------------------------ */
// Safe, dependency-free Markdown renderer for AI answers (headings, lists, tables,
// quotes, code, links). All input is HTML-escaped first, so output cannot inject markup.
function renderMarkdown(source) {
  const esc = (t) => t.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  const inline = (t) => {
    const codes = [];
    t = t.replace(/`([^`\n]+)`/g, (_, c) => { codes.push(c); return "\u0000" + (codes.length - 1) + "\u0000"; });
    t = t.replace(/\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>')
         .replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>")
         .replace(/(^|[^*\w])\*([^*\n]+)\*(?!\w)/g, "$1<em>$2</em>");
    return t.replace(/\u0000(\d+)\u0000/g, (_, i) => "<code>" + codes[Number(i)] + "</code>");
  };
  const lines = esc(String(source || "").replace(/\r\n?/g, "\n")).split("\n");
  const out = [];
  let i = 0;
  const isTableSep = (l) => /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)+\|?\s*$/.test(l || "");
  const cells = (l) => l.trim().replace(/^\||\|$/g, "").split("|").map((c) => c.trim());
  while (i < lines.length) {
    const line = lines[i];
    let m;
    if ((m = line.match(/^\s*```([\w-]*)\s*$/))) {
      const code = [];
      i++;
      while (i < lines.length && !/^\s*```\s*$/.test(lines[i])) code.push(lines[i++]);
      i++;
      out.push("<pre><code>" + code.join("\n") + "</code></pre>");
    } else if (!line.trim()) {
      i++;
    } else if ((m = line.match(/^(#{1,4})\s+(.*)$/))) {
      const level = Math.min(m[1].length + 2, 6);
      out.push("<h" + level + ">" + inline(m[2]) + "</h" + level + ">");
      i++;
    } else if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(line)) {
      out.push("<hr>");
      i++;
    } else if (line.includes("|") && isTableSep(lines[i + 1])) {
      const head = cells(line);
      i += 2;
      const rows = [];
      while (i < lines.length && lines[i].includes("|") && lines[i].trim()) rows.push(cells(lines[i++]));
      out.push("<div class=\"table-scroll\"><table><thead><tr>" + head.map((c) => "<th>" + inline(c) + "</th>").join("") + "</tr></thead><tbody>" +
        rows.map((r) => "<tr>" + r.map((c) => "<td>" + inline(c) + "</td>").join("") + "</tr>").join("") + "</tbody></table></div>");
    } else if (/^\s*&gt;\s?/.test(line)) {
      const q = [];
      while (i < lines.length && /^\s*&gt;\s?/.test(lines[i])) q.push(lines[i++].replace(/^\s*&gt;\s?/, ""));
      out.push("<blockquote>" + inline(q.join("<br>")) + "</blockquote>");
    } else if (/^\s*([-*•]|\d+[.)])\s+/.test(line)) {
      const ordered = /^\s*\d+[.)]\s+/.test(line);
      const items = [];
      while (i < lines.length && /^\s*([-*•]|\d+[.)])\s+/.test(lines[i])) {
        let item = lines[i++].replace(/^\s*([-*•]|\d+[.)])\s+/, "");
        while (i < lines.length && /^\s{2,}\S/.test(lines[i]) && !/^\s*([-*•]|\d+[.)])\s+/.test(lines[i])) item += "<br>" + lines[i++].trim();
        items.push("<li>" + inline(item) + "</li>");
      }
      out.push((ordered ? "<ol>" : "<ul>") + items.join("") + (ordered ? "</ol>" : "</ul>"));
    } else {
      const para = [];
      while (i < lines.length && lines[i].trim() && !/^(#{1,4}\s|\s*```|\s*([-*•]|\d+[.)])\s+|\s*&gt;)/.test(lines[i]) && !(lines[i].includes("|") && isTableSep(lines[i + 1]))) para.push(lines[i++]);
      out.push("<p>" + inline(para.join("<br>")) + "</p>");
    }
  }
  return out.join("");
}

// Fill-in starter templates for each assistant. [Square brackets] mark what to replace.
const STARTERS = {
  study: [
    ["Explain a topic", "Explain [topic] to a [beginner / high-school / university] student. Start with the big idea in one sentence, then a simple explanation, a real-world example and a 3-question check."],
    ["Quiz me", "Quiz me on [topic] at [easy / medium / hard] level. Ask one question at a time, wait for my answer, then give feedback."],
    ["Flashcards", "Create 10 flashcards (question and answer in a table) from these notes:\n[paste notes]"],
    ["Study plan", "Build a day-by-day study plan for [subject/exam] on [date]. I can study [hours] per day and I'm weakest in [topics]."],
    ["Worked example", "Show a fully worked example of [problem type], explaining the reason for every step."]
  ],
  career: [
    ["CV bullets", "Turn this experience into 4 achievement-focused CV bullets (action, result, number where possible):\n[describe role and what you did]"],
    ["Cover letter", "Write a 300-word cover letter for the role of [job title] at [company]. My background: [summary]. Job description:\n[paste]"],
    ["Mock interview", "Run a mock interview for [job title]. Ask one question at a time, then give feedback and a stronger sample answer."],
    ["Career path", "I currently work as [role] with skills in [skills]. Compare 3 realistic career paths toward [goal] and list the skill gaps and next steps."],
    ["Salary talk", "Help me prepare to negotiate a salary for [job title] in [city/country]. My offer is [amount]; my strengths are [list]."]
  ],
  business: [
    ["Stress-test idea", "Stress-test this business idea: [idea]. Target customer: [who]. List the biggest risks and the cheapest test for each."],
    ["Lean canvas", "Create a one-page lean canvas for [business idea] aimed at [customer] in [location]."],
    ["Marketing plan", "Build a 30-day marketing plan for [product/service] with a budget of [amount]. Include channels, weekly actions and metrics."],
    ["Product copy", "Write a persuasive product description (60-100 words) for [product]. Audience: [who]. Key benefits: [list]. Tone: [tone]."],
    ["Pricing", "Suggest 3 pricing options for [product/service] with costs of [costs] and competitors charging [prices]. Show the simple maths."]
  ],
  documents: [
    ["Summarise", "Summarise the following in 5 bullet points, then give a one-sentence takeaway:\n[paste text]"],
    ["Key facts", "Extract all names, dates, amounts and obligations from this text into a table:\n[paste text]"],
    ["Action items", "List the action items, owners and deadlines, plus any open questions, from this text:\n[paste text]"],
    ["Plain language", "Explain this in plain language for someone with no background, and flag anything I should double-check with a professional:\n[paste text]"],
    ["Outline", "Turn these notes into a clear, organised outline with headings:\n[paste notes]"]
  ],
  website: [
    ["Sitemap", "Plan the pages for a [type] website for [business]. Give a sitemap, the sections on each page and a call to action for each."],
    ["Homepage copy", "Write homepage copy for [business] serving [audience]: headline, subheadline, 3 benefits and a call to action. Tone: [tone]."],
    ["UX review", "Review this page for usability, accessibility, mobile layout and SEO. List issues by priority with a fix for each:\n[paste HTML or describe the page]"],
    ["SEO basics", "Suggest SEO titles (under 60 characters) and meta descriptions (under 155) for these pages: [list pages]. Keywords: [keywords]."],
    ["Launch checklist", "Give me a launch checklist for my website [type], covering content, performance, security, analytics and promotion."]
  ],
  support: [
    ["Reply to customer", "Draft a calm, empathetic reply to this customer message. Tone: [tone]. What I can offer: [solution].\n[paste message]"],
    ["FAQ", "Create 10 FAQs with short answers for [product/service], grouped by theme. Key policies: [list]."],
    ["Workflow", "Design a support workflow for [team size] handling [channels]. Include triage levels, response-time targets and escalation steps."],
    ["Canned replies", "Write 6 reusable reply templates for [common issues], each with [placeholders] for personal details."],
    ["Complaint summary", "Summarise this complaint, identify the root cause and list what we should fix:\n[paste complaint]"]
  ],
  data: [
    ["Understand data", "Here is my data: [paste a sample or describe columns]. Explain what it contains, flag quality issues and suggest 3 questions it could answer."],
    ["Metrics", "My goal is [goal] for [business/project]. Suggest the 6 most useful metrics, how to calculate each, and what a good result looks like."],
    ["Pick charts", "I want to show [insight] to [audience]. Recommend the best chart type(s) and what goes on each axis."],
    ["Clean data", "Give me a step-by-step checklist (with Excel/Sheets formulas) to clean this kind of data: [describe]."],
    ["Findings summary", "Write a short, plain-language findings summary for a manager based on these results:\n[paste results]"]
  ]
};

document.addEventListener("DOMContentLoaded", () => {
  const list = document.getElementById("starterList");
  const form = document.getElementById("chatForm");
  const input = document.getElementById("chatInput");
  if (!list || !form || !input) return;
  (STARTERS[form.dataset.module] || []).forEach(([label, text]) => {
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = label;
    button.addEventListener("click", () => {
      input.value = text;
      input.dispatchEvent(new Event("input"));
      input.focus();
      const start = text.indexOf("[");
      const end = text.indexOf("]", start);
      if (start >= 0 && end > start) input.setSelectionRange(start, end + 1);
      input.scrollIntoView({ block: "nearest", behavior: "smooth" });
    });
    list.appendChild(button);
  });
  if (!list.children.length) list.closest(".aside-card")?.remove();
});

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
    const formatAnswer=value=>renderMarkdown(value);
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
  try { saved = localStorage.getItem(key) || localStorage.getItem("connectai-theme") || "system"; } catch (_) {}
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
      .catch((error) => console.warn("ConnectAI Hub service worker unavailable:", error));
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


// Composer ergonomics: auto-growing textarea, Enter sends, Shift+Enter adds a line.
(() => {
  const input = document.getElementById("chatInput");
  const form = document.getElementById("chatForm");
  if (!input || !form) return;
  const fit = () => { input.style.height = "auto"; input.style.height = Math.min(input.scrollHeight, 220) + "px"; };
  input.addEventListener("input", fit);
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing && window.matchMedia("(min-width: 700px)").matches) {
      e.preventDefault();
      if (input.value.trim()) form.requestSubmit();
    }
  });
  form.addEventListener("submit", () => setTimeout(fit, 0));
  fit();
})();
