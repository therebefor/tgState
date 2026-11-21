package control

import (
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"csz.net/tgstate/assets"
	"csz.net/tgstate/conf"
	"csz.net/tgstate/utils"
)

// UploadImageAPI 上传图片api
func UploadImageAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodPost {
		// 获取上传的文件
		file, header, err := r.FormFile("image")
		if err != nil {
			errJsonMsg("无法获取上传的文件，请重新选择后再试", w)
			return
		}
		defer file.Close()
		if conf.Mode != "p" && header.Size > 20*1024*1024 {
			// 图床模式：单张图片不超过 20 MB；网盘模式（mode=p）不受此限制
			errJsonMsg("图片不能超过 20 MB", w)
			return
		}
		// 检查文件类型
		allowedExts := []string{".jpg", ".jpeg", ".png"}
		ext := filepath.Ext(header.Filename)
		valid := false
		for _, allowedExt := range allowedExts {
			if ext == allowedExt {
				valid = true
				break
			}
		}
		if conf.Mode != "p" && !valid {
			errJsonMsg("仅支持 JPG / PNG 格式的图片", w)
			return
		}
		res := conf.UploadResponse{
			Code:    0,
			Message: "上传失败，请稍后重试",
		}
		fileID, msgID, chatID := utils.UpDocument(utils.TgFileData(header.Filename, file))
		img := conf.FileRoute + fileID
		if fileID != "" {
			link := strings.TrimSuffix(conf.BaseUrl, "/") + img
			res = conf.UploadResponse{
				Code:    1,
				Message: img,
				ImgUrl:  link,
			}
			// 同步发送：Vercel 上函数返回响应后实例会被冻结，不能用 goroutine
			utils.ReplyLink(chatID, msgID, link)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(res)
		return
	}

	// 如果不是POST请求，返回错误响应
	http.Error(w, "请求方法不支持，请使用 POST", http.StatusMethodNotAllowed)
}
func errJsonMsg(msg string, w http.ResponseWriter) {
	// 这里示例直接返回JSON响应
	response := conf.UploadResponse{
		Code:    0,
		Message: msg,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
func D(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	id := strings.TrimPrefix(path, conf.FileRoute)
	if id == "" {
		// 设置响应的状态码为 404
		w.WriteHeader(http.StatusNotFound)
		// 写入响应内容
		w.Write([]byte("文件不存在"))
		return
	}

	// 发起HTTP GET请求来获取Telegram图片
	fileUrl, _ := utils.GetDownloadUrl(id)
	resp, err := http.Get(fileUrl)
	if err != nil {
		http.Error(w, "获取文件内容失败，请稍后重试", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Disposition", "inline") // 设置为 "inline" 以支持在线播放
	// 检查Content-Type是否为图片类型
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/octet-stream") {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("文件不存在"))
		return
	}
	contentLength, err := strconv.Atoi(resp.Header.Get("Content-Length"))
	if err != nil {
		log.Println("获取Content-Length出错:", err)
		return
	}
	buffer := make([]byte, contentLength)
	n, err := resp.Body.Read(buffer)
	defer resp.Body.Close()
	if err != nil && err != io.ErrUnexpectedEOF {
		log.Println("读取响应主体数据时发生错误:", err)
		return
	}
	// 输出文件内容到控制台
	if string(buffer[:12]) == "tgstate-blob" {
		content := string(buffer)
		lines := strings.Split(content, "\n")
		log.Println("分块文件:" + lines[1])
		var fileSize string
		var startLine = 2
		if strings.HasPrefix(lines[2], "size") {
			fileSize = lines[2][len("size"):]
			startLine = startLine + 1
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+lines[1]+"\"")
		w.Header().Set("Content-Length", fileSize)
		for i := startLine; i < len(lines); i++ {
			fileStatus := false
			var fileUrl string
			var reTry = 0
			for !fileStatus {
				if reTry > 0 {
					time.Sleep(5 * time.Second)
				}
				reTry = reTry + 1
				fileUrl, fileStatus = utils.GetDownloadUrl(strings.ReplaceAll(lines[i], " ", ""))
			}
			blobResp, err := http.Get(fileUrl)
			if err != nil {
				http.Error(w, "获取文件内容失败，请稍后重试", http.StatusInternalServerError)
				return
			}
			_, err = io.Copy(w, blobResp.Body)
			blobResp.Body.Close()
			if err != nil {
				log.Println("写入响应主体数据时发生错误:", err)
				return
			}
		}
	} else {
		// 使用DetectContentType函数检测文件类型
		w.Header().Set("Content-Type", http.DetectContentType(buffer))
		_, err = w.Write(buffer[:n])
		if err != nil {
			http.Error(w, "写入文件内容失败，请稍后重试", http.StatusInternalServerError)
			log.Println(http.StatusInternalServerError)
			return
		}
		_, err = io.Copy(w, resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Println(http.StatusInternalServerError)
			return
		}
	}
}

// Index 首页
func Index(w http.ResponseWriter, r *http.Request) {
	htmlPath := "templates/images.tmpl"
	if conf.Mode == "p" {
		htmlPath = "templates/files.tmpl"
	}
	file, err := assets.Templates.ReadFile(htmlPath)
	if err != nil {
		http.Error(w, "页面模板不存在", http.StatusNotFound)
		return
	}
	// 读取头部模板
	headerFile, err := assets.Templates.ReadFile("templates/header.tmpl")
	if err != nil {
		http.Error(w, "页面头部模板不存在", http.StatusNotFound)
		return
	}

	// 读取页脚模板
	footerFile, err := assets.Templates.ReadFile("templates/footer.tmpl")
	if err != nil {
		http.Error(w, "页面底部模板不存在", http.StatusNotFound)
		return
	}

	// 创建HTML模板并包括头部
	tmpl := template.New("html")
	tmpl, err = tmpl.Parse(string(headerFile))
	if err != nil {
		http.Error(w, "页面头部模板解析失败", http.StatusInternalServerError)
		return
	}

	// 包括主HTML内容
	tmpl, err = tmpl.Parse(string(file))
	if err != nil {
		http.Error(w, "页面模板解析失败", http.StatusInternalServerError)
		return
	}

	// 包括页脚
	tmpl, err = tmpl.Parse(string(footerFile))
	if err != nil {
		http.Error(w, "页面底部模板解析失败", http.StatusInternalServerError)
		return
	}

	// 直接将HTML内容发送给客户端
	w.Header().Set("Content-Type", "text/html")
	err = tmpl.Execute(w, nil)
	if err != nil {
		http.Error(w, "页面渲染失败", http.StatusInternalServerError)
	}
}

func Pwd(w http.ResponseWriter, r *http.Request) {
	// 输出 HTML 表单
	if r.Method != http.MethodPost {
		file, err := assets.Templates.ReadFile("templates/pwd.tmpl")
		if err != nil {
			http.Error(w, "页面模板不存在", http.StatusNotFound)
			return
		}
		// 读取头部模板
		headerFile, err := assets.Templates.ReadFile("templates/header.tmpl")
		if err != nil {
			http.Error(w, "页面头部模板不存在", http.StatusNotFound)
			return
		}

		// 创建HTML模板并包括头部
		tmpl := template.New("html")
		if tmpl, err = tmpl.Parse(string(headerFile)); err != nil {
			http.Error(w, "页面头部模板解析失败", http.StatusInternalServerError)
			return
		}

		// 包括主HTML内容
		if tmpl, err = tmpl.Parse(string(file)); err != nil {
			http.Error(w, "页面模板解析失败", http.StatusInternalServerError)
			return
		}

		// 直接将HTML内容发送给客户端
		w.Header().Set("Content-Type", "text/html")
		if err := tmpl.Execute(w, nil); err != nil {
			http.Error(w, "页面渲染失败", http.StatusInternalServerError)
		}
		return
	}
	// 设置cookie
	cookie := http.Cookie{
		Name:  "p",
		Value: r.FormValue("p"),
	}
	http.SetCookie(w, &cookie)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// redeemReward 表示一个兑换码对应的奖励
type redeemReward struct {
	extra     int
	unlimited bool
}

// parseRedeemCodes 解析环境变量 codes：
// 多个兑换码用英文逗号分隔，单个格式为“兑换码:奖励”，
// 奖励为正整数（额外上传次数）或 unlimited/u/inf/∞（不限次数）。
func parseRedeemCodes(raw string) map[string]redeemReward {
	codes := make(map[string]redeemReward)
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.ToLower(strings.TrimSpace(parts[1]))
		if key == "" {
			continue
		}
		if value == "unlimited" || value == "u" || value == "inf" || value == "∞" {
			codes[key] = redeemReward{unlimited: true}
			continue
		}
		amount, err := strconv.Atoi(value)
		if err != nil || amount <= 0 {
			continue
		}
		codes[key] = redeemReward{extra: amount}
	}
	return codes
}

// Redeem 校验兑换码并返回奖励。
// 服务端无状态：奖励由前端记录，兑换码本身不做使用次数记录。
func Redeem(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodPost {
		http.Error(w, "请求方法不支持，请使用 POST", http.StatusMethodNotAllowed)
		return
	}
	code := strings.ToLower(strings.TrimSpace(r.FormValue("code")))
	res := conf.RedeemResponse{Code: 0, Message: "兑换码无效，请检查后重试"}
	if strings.TrimSpace(conf.Codes) == "" {
		res.Message = "未开启兑换码功能"
	} else if code == "" {
		res.Message = "请输入兑换码"
	} else if reward, ok := parseRedeemCodes(conf.Codes)[code]; ok {
		if reward.unlimited {
			res = conf.RedeemResponse{Code: 1, Message: "兑换成功", Unlimited: true}
		} else {
			res = conf.RedeemResponse{Code: 1, Message: "兑换成功", Extra: reward.extra}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(res)
}

func Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 只有当密码设置并且不为"none"时，才进行检查
		if conf.Pass != "" && conf.Pass != "none" {
			if strings.HasPrefix(r.URL.Path, "/api") && r.URL.Query().Get("pass") == conf.Pass {
				return
			}
			if cookie, err := r.Cookie("p"); err != nil || cookie.Value != conf.Pass {
				http.Redirect(w, r, "/pwd", http.StatusSeeOther)
				return
			}
		}
		next(w, r)
	}
}
