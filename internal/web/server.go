package web

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectai-hub/internal/auth"
)

type Module struct { Slug, Name, Description, Icon, Color string; Features []string }
type User struct { ID int64; Name, Email string }
type PageData struct {
	Title, Active, CSRF, Error, Name string
	Modules []Module
	Module Module
	User *User
}
type Server struct { mux *http.ServeMux; tpl *template.Template; db *sql.DB; modules []Module }

func New(db *sql.DB) (*Server,error) {
	t,err:=template.ParseGlob(filepath.Join("templates","*.html")); if err!=nil{return nil,err}
	s:=&Server{mux:http.NewServeMux(),tpl:t,db:db,modules:[]Module{
		{"study","Study AI","Understand difficult topics, practise concepts and build confidence as you learn.","✦","violet",[]string{"Explain topics simply","Create practice questions","Summarise learning materials"}},
		{"career","Career AI","Prepare professional career materials and practise for your next opportunity.","↗","blue",[]string{"Improve CV content","Practise interview questions","Explore career paths"}},
		{"business","Business AI","Turn business ideas into clearer plans, marketing content and practical next steps.","◈","orange",[]string{"Develop business ideas","Create marketing plans","Draft product descriptions"}},
		{"documents","Document AI","Work with documents and get useful answers from the information they contain.","▤","green",[]string{"Ask questions about files","Summarise documents","Extract key information"}},
		{"website","Website AI","Plan websites, organise pages and shape a digital product.","⌘","pink",[]string{"Plan website structure","Draft page content","Explore interface ideas"}},
		{"support","Support AI","Design helpful customer-support workflows and responses.","◎","cyan",[]string{"Draft support replies","Organise FAQs","Design support workflows"}},
		{"data","Data AI","Make data easier to understand with analysis plans, summaries and visual insights.","▥","purple",[]string{"Explore datasets","Summarise findings","Plan charts and reports"}},
	}}
	s.routes();return s,nil
}
func(s *Server)Handler()http.Handler{return s.mux}
func(s *Server)routes(){
	s.mux.Handle("/static/",http.StripPrefix("/static/",http.FileServer(http.Dir("./static"))))
	s.mux.HandleFunc("GET /health",func(w http.ResponseWriter,r *http.Request){w.Header().Set("Content-Type","text/plain; charset=utf-8");_,_=w.Write([]byte("ok"))})
	s.mux.HandleFunc("GET /{$}",s.home)
	s.mux.HandleFunc("GET /dashboard",s.dashboard)
	s.mux.HandleFunc("GET /modules",s.dashboard)
	s.mux.HandleFunc("GET /modules/{slug}",s.module)
	s.mux.HandleFunc("GET /register",s.registerPage)
	s.mux.HandleFunc("POST /register",s.register)
	s.mux.HandleFunc("GET /login",s.loginPage)
	s.mux.HandleFunc("POST /login",s.login)
	s.mux.HandleFunc("POST /logout",s.logout)
}
func(s *Server)render(w http.ResponseWriter,name string,d PageData){w.Header().Set("Content-Type","text/html; charset=utf-8");if err:=s.tpl.ExecuteTemplate(w,name,d);err!=nil{http.Error(w,"Unable to render page",500)}}
func(s *Server)csrf(w http.ResponseWriter,r *http.Request)string{
	c,err:=r.Cookie("connectai_csrf");if err==nil&&len(c.Value)>=32{return c.Value}
	b:=make([]byte,32);if _,err=rand.Read(b);err!=nil{return ""}
	token:=base64.RawURLEncoding.EncodeToString(b)
	http.SetCookie(w,&http.Cookie{Name:"connectai_csrf",Value:token,Path:"/",SameSite:http.SameSiteLaxMode,Secure:os.Getenv("APP_ENV")=="production",MaxAge:3600})
	return token
}
func(s *Server)validCSRF(r *http.Request)bool{c,e:=r.Cookie("connectai_csrf");return e==nil&&c.Value!=""&&r.FormValue("csrf_token")==c.Value}
func(s *Server)currentUser(r *http.Request)*User{
	c,err:=r.Cookie("connectai_session");if err!=nil||c.Value==""{return nil}
	sum:=sha256.Sum256([]byte(c.Value));hash:=hex.EncodeToString(sum[:])
	var u User
	err=s.db.QueryRow(`SELECT u.id,u.name,u.email FROM users u JOIN sessions s ON s.user_id=u.id WHERE s.token_hash=$1 AND s.expires_at>NOW()`,hash).Scan(&u.ID,&u.Name,&u.Email)
	if err!=nil{return nil};return &u
}
func(s *Server)createSession(w http.ResponseWriter,userID int64)error{
	raw:=make([]byte,32);if _,err:=rand.Read(raw);err!=nil{return err}
	token:=base64.RawURLEncoding.EncodeToString(raw);sum:=sha256.Sum256([]byte(token))
	_,err:=s.db.Exec(`INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,$3)`,userID,hex.EncodeToString(sum[:]),time.Now().Add(7*24*time.Hour))
	if err!=nil{return err}
	http.SetCookie(w,&http.Cookie{Name:"connectai_session",Value:token,Path:"/",HttpOnly:true,Secure:os.Getenv("APP_ENV")=="production",SameSite:http.SameSiteLaxMode,Expires:time.Now().Add(7*24*time.Hour),MaxAge:7*24*60*60})
	return nil
}
func(s *Server)home(w http.ResponseWriter,r *http.Request){s.render(w,"home.html",PageData{Title:"One Hub. Endless Possibilities.",Modules:s.modules,Active:"home",User:s.currentUser(r),CSRF:s.csrf(w,r)})}
func(s *Server)dashboard(w http.ResponseWriter,r *http.Request){
	u:=s.currentUser(r);if u==nil{http.Redirect(w,r,"/login",http.StatusSeeOther);return}
	s.render(w,"dashboard.html",PageData{Title:"Your workspace",Modules:s.modules,Active:"dashboard",User:u,CSRF:s.csrf(w,r)})
}
func(s *Server)module(w http.ResponseWriter,r *http.Request){
	u:=s.currentUser(r);if u==nil{http.Redirect(w,r,"/login",http.StatusSeeOther);return}
	for _,m:=range s.modules{if m.Slug==r.PathValue("slug"){s.render(w,"module.html",PageData{Title:m.Name,Modules:s.modules,Module:m,Active:m.Slug,User:u,CSRF:s.csrf(w,r)});return}}
	http.NotFound(w,r)
}
func(s *Server)registerPage(w http.ResponseWriter,r *http.Request){s.render(w,"register.html",PageData{Title:"Create account",CSRF:s.csrf(w,r)})}
func(s *Server)loginPage(w http.ResponseWriter,r *http.Request){s.render(w,"login.html",PageData{Title:"Log in",CSRF:s.csrf(w,r)})}
func(s *Server)register(w http.ResponseWriter,r *http.Request){
	if err:=r.ParseForm();err!=nil{http.Error(w,"Invalid form",400);return};if !s.validCSRF(r){http.Error(w,"Your form expired. Please try again.",400);return}
	name:=strings.TrimSpace(r.FormValue("name"));email:=strings.ToLower(strings.TrimSpace(r.FormValue("email")));password:=r.FormValue("password")
	if len(name)<2||len(name)>80||!strings.Contains(email,"@")||len(email)>254{ s.render(w,"register.html",PageData{Title:"Create account",CSRF:s.csrf(w,r),Error:"Enter a valid name and email address."});return }
	hash,err:=auth.HashPassword(password);if err!=nil{s.render(w,"register.html",PageData{Title:"Create account",CSRF:s.csrf(w,r),Error:err.Error()});return}
	var id int64
	err=s.db.QueryRow(`INSERT INTO users(name,email,password_hash) VALUES($1,$2,$3) RETURNING id`,name,email,hash).Scan(&id)
	if err!=nil{s.render(w,"register.html",PageData{Title:"Create account",CSRF:s.csrf(w,r),Error:"That email may already be registered. Check it or log in."});return}
	if err=s.createSession(w,id);err!=nil{http.Error(w,"Account created, but sign-in failed. Please log in.",500);return}
	http.Redirect(w,r,"/dashboard",http.StatusSeeOther)
}
func(s *Server)login(w http.ResponseWriter,r *http.Request){
	if err:=r.ParseForm();err!=nil{http.Error(w,"Invalid form",400);return};if !s.validCSRF(r){http.Error(w,"Your form expired. Please try again.",400);return}
	email:=strings.ToLower(strings.TrimSpace(r.FormValue("email")));password:=r.FormValue("password")
	var id int64;var hash string
	err:=s.db.QueryRow(`SELECT id,password_hash FROM users WHERE email=$1`,email).Scan(&id,&hash)
	if err!=nil||!auth.VerifyPassword(password,hash){s.render(w,"login.html",PageData{Title:"Log in",CSRF:s.csrf(w,r),Error:"Email or password is incorrect."});return}
	if err=s.createSession(w,id);err!=nil{http.Error(w,"Unable to create session",500);return}
	http.Redirect(w,r,"/dashboard",http.StatusSeeOther)
}
func(s *Server)logout(w http.ResponseWriter,r *http.Request){
	if r.Method!="POST"{http.Error(w,"Method not allowed",405);return}
	if !s.validCSRF(r){http.Error(w,"Your form expired. Please try again.",400);return}
	if c,err:=r.Cookie("connectai_session");err==nil{sum:=sha256.Sum256([]byte(c.Value));_,_=s.db.Exec(`DELETE FROM sessions WHERE token_hash=$1`,hex.EncodeToString(sum[:] ))}
	http.SetCookie(w,&http.Cookie{Name:"connectai_session",Value:"",Path:"/",HttpOnly:true,Secure:os.Getenv("APP_ENV")=="production",SameSite:http.SameSiteLaxMode,MaxAge:-1})
	http.Redirect(w,r,"/",http.StatusSeeOther)
}
