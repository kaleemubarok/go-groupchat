package main

import (
	"crypto/sha256"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/lolmourne/go-groupchat/resource/acc"
	"github.com/lolmourne/go-groupchat/resource/groupchat"
	groupchat2 "github.com/lolmourne/go-groupchat/usecase/groupchat"
	"github.com/lolmourne/go-groupchat/usecase/userauth"
	"log"
	"math/rand"
	"net/http"
	"strconv"
)

var db *sqlx.DB
var dbResource acc.DBItf
var dbRoomResource groupchat.DBItf
var userAuthUsecase userauth.UsecaseItf
var groupChatUsecase groupchat2.UsecaseItf

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	dbInit, err := sqlx.Connect("postgres", "host=34.101.216.10 user=skilvul password=skilvul123apa dbname=skilvul-groupchat sslmode=disable")
	if err != nil {
		log.Fatalln(err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     "34.101.216.10:6379",
		Password: "skilvulredis", // no password set
		DB:       0,              // use default DB
	})

	dbRsc := acc.NewDBResource(dbInit)
	dbRsc = acc.NewRedisResource(rdb, dbRsc)

	dbRoomRsc := groupchat.NewRedisResource(rdb, groupchat.NewDBResource(dbInit))

	dbResource = dbRsc
	dbRoomResource = dbRoomRsc
	db = dbInit

	userAuthUsecase = userauth.NewUsecase(dbRsc, "signedK3y")
	groupChatUsecase = groupchat2.NewUseCase(dbRoomRsc, "signedK3y")

	r := gin.Default()
	r.POST("/register", register)
	r.POST("/login", login)
	r.GET("/usr/:user_id", getUser)
	r.GET("/profile/:username", getProfile)
	r.PUT("/profile", validateSession(updateProfile))
	r.PUT("/password", validateSession(changePassword))

	// untuk PR
	r.PUT("/groupchat", validateSession(joinRoom))
	r.POST("/groupchat", validateSession(createRoom))
	r.GET("/joined", validateSession(getJoinedRoom))
	r.Run()
}

func validateSession(handlerFunc gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		accessToken := c.Request.Header["X-Access-Token"]

		if len(accessToken) < 1 {
			c.JSON(403, StandardAPIResponse{
				Err:     "No access token provided",
				Message: "Forbidden",
			})
			return
		}

		userID, err := userAuthUsecase.ValidateSession(accessToken[0])
		if err != nil {
			c.JSON(400, StandardAPIResponse{
				Err: err.Error(),
			})
			return
		}
		c.Set("uid", userID)
		handlerFunc(c)
	}
}

func register(c *gin.Context) {
	username := c.Request.FormValue("username")
	password := c.Request.FormValue("password")
	confirmPassword := c.Request.FormValue("confirm_password")

	err := userAuthUsecase.Register(username, password, confirmPassword)
	if err != nil {
		c.JSON(400, StandardAPIResponse{
			Err:     err.Error(),
			Message: "Failed",
		})
		return
	}

	c.JSON(201, StandardAPIResponse{
		Err:     "null",
		Message: "Success create new user",
	})
}

func login(c *gin.Context) {
	username := c.Request.FormValue("username")
	password := c.Request.FormValue("password")

	user, err := userAuthUsecase.Login(username, password)
	if err != nil {
		c.JSON(400, StandardAPIResponse{
			Err:     err.Error(),
			Message: "Failed",
		})
		return
	}

	c.JSON(200, StandardAPIResponse{
		Data: user,
	})
}

func getUser(c *gin.Context) {
	uid := c.Param("user_id")

	userID, err := strconv.ParseInt(uid, 10, 64)
	if err != nil {
		log.Println(err)
		return
	}

	user, err := dbResource.GetUserByUserID(userID)
	if err != nil {
		c.JSON(400, StandardAPIResponse{
			Err: "Unauthorized",
		})
		return
	}

	if user.UserID == 0 {
		c.JSON(http.StatusNotFound, StandardAPIResponse{
			Err: "user not found",
		})
		return
	}

	user.Salt=""
	user.Password=""

	c.JSON(200, StandardAPIResponse{
		Err:  "null",
		Data: user,
	})
}

func getProfile(c *gin.Context) {
	username := c.Param("username")

	user, err := dbResource.GetUserByUserName(username)
	if err != nil {
		c.JSON(400, StandardAPIResponse{
			Err: "Unauthorized",
		})
		return
	}

	if user.UserID == 0 {
		c.JSON(http.StatusNotFound, StandardAPIResponse{
			Err: "user not found",
		})
		return
	}

	user.Password=""
	user.Salt=""

	c.JSON(200, StandardAPIResponse{
		Err:  "null",
		Data: user,
	})
}

func updateProfile(c *gin.Context) {
	userID := c.GetInt64("uid")
	if userID < 1 {
		c.JSON(400, StandardAPIResponse{
			Err: "no user founds",
		})
		return
	}

	profilepic := c.Request.FormValue("profile_pic")
	err := dbResource.UpdateProfile(userID, profilepic)
	if err != nil {
		c.JSON(400, StandardAPIResponse{
			Err: err.Error(),
		})
		return
	}

	c.JSON(201, StandardAPIResponse{
		Err:     "null",
		Message: "Success update profile picture",
	})

}

func changePassword(c *gin.Context) {
	userID := c.GetInt64("uid")

	oldpass := c.Request.FormValue("old_password")
	newpass := c.Request.FormValue("new_password")

	user, err := dbResource.GetUserByUserID(userID)
	if err != nil {
		c.JSON(400, StandardAPIResponse{
			Err: err.Error(),
		})
		return
	}

	oldpass += user.Salt
	h := sha256.New()
	h.Write([]byte(oldpass))
	hashedOldPassword := fmt.Sprintf("%x", h.Sum(nil))

	if user.Password != hashedOldPassword {
		c.JSON(401, StandardAPIResponse{
			Err: "old password is wrong!",
		})
		return
	}

	//new pass
	salt := RandStringBytes(32)
	newpass += salt

	h = sha256.New()
	h.Write([]byte(newpass))
	hashedNewPass := fmt.Sprintf("%x", h.Sum(nil))

	err2 := dbResource.UpdateUserPassword(userID, hashedNewPass)

	if err2 != nil {
		c.JSON(400, StandardAPIResponse{
			Err: err.Error(),
		})
		return
	}

	c.JSON(201, StandardAPIResponse{
		Err:     "null",
		Message: "Success update password",
	})

}

func createRoom(c *gin.Context) {
	name := c.Request.FormValue("name")
	desc := c.Request.FormValue("desc")
	categoryId := c.Request.FormValue("category_id")
	adminId := c.GetInt64("uid") //by default the one who create will be group admin

	adminStr := strconv.FormatInt(adminId, 10)

	_,err:=groupChatUsecase.CreateGroupchat(name,adminStr,desc,categoryId)

	if err != nil {
		c.JSON(400, StandardAPIResponse{
			Err: err.Error(),
		})
		return
	}

	c.JSON(201, StandardAPIResponse{
		Err:     "null",
		Message: "Success create new groupchat",
	})
}

func joinRoom(c *gin.Context) {
	userID := c.GetInt64("uid")
	if userID < 1 {
		c.JSON(400, StandardAPIResponse{
			Err: "user not found",
		})
		return
	}

	reqRoomID := c.Request.FormValue("room_id")
	roomID,err := strconv.ParseInt(reqRoomID,10,64)
	if err != nil {
		c.JSON(400, StandardAPIResponse{
			Err: "wrong room id",
		})
		return
	}

	err = groupChatUsecase.JoinRoom(roomID, userID)

	if err != nil {
		c.JSON(400, StandardAPIResponse{
			Err: err.Error(),
		})
		return
	}

	c.JSON(201, StandardAPIResponse{
		Err:     "null",
		Message: "Success join to group chat with ID " + reqRoomID,
	})
}

func getJoinedRoom(c *gin.Context)  {
	userID := c.GetInt64("uid")
	rooms,err := dbRoomResource.GetJoinedRoom(userID)

	if err != nil {
		c.JSON(400, StandardAPIResponse{
			Err: "Unauthorized",
		})
		return
	}

	c.JSON(200, StandardAPIResponse{
		Err:  "null",
		Data: rooms,
	})
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func RandStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

type StandardAPIResponse struct {
	Err     string      `json:"err"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}
