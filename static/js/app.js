document.addEventListener("DOMContentLoaded", () => {
  const toggle=document.querySelector(".menu-toggle"), nav=document.querySelector(".nav-links");
  if(toggle&&nav) toggle.addEventListener("click",()=>{const open=nav.classList.toggle("open");toggle.setAttribute("aria-expanded",String(open));});
  const search=document.getElementById("moduleSearch"), cards=document.querySelectorAll("#moduleGrid [data-search]");
  if(search) search.addEventListener("input",()=>{const q=search.value.trim().toLowerCase();cards.forEach(card=>card.hidden=!card.dataset.search.toLowerCase().includes(q));});
});
