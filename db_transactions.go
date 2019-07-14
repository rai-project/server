package server

import (
	"github.com/rai-project/config"
	"github.com/rai-project/database"
	"github.com/rai-project/database/mongodb"
	"github.com/rai-project/model"
	"github.com/rai-project/utils"
	"gopkg.in/mgo.v2/bson"
	"time"
)

type Ranking struct {
	model.Base    `json:",inline" bson:",inline"`
	ExpireAt      time.Time `json:"expireat",omitempty`
	Username      string    `json:"username,omitempty"`
	UserAccessKey string    `json:"user_accesskey,omitempty"`
	ProjectURL    string    `json:"project_url,omitempty"`
}

type JobResponseBody struct {
	ID      bson.ObjectId `json:"_id" bson:"_id" gorm:"primary_key" toml:"id.omitempty" validate:"required"`
	Ranking `json:",inline" bson:",inline"`
}

func (w *WorkRequest) RecordJob() error {

	body := JobResponseBody{
		ID: w.ID,
		Ranking: Ranking{
			Base: model.Base{
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}

	body.Username = w.JobRequest.User.Username
	body.UserAccessKey = w.JobRequest.User.AccessKey
	body.ProjectURL = w.JobRequest.UploadKey
	//Delete entry after 1 year
	body.ExpireAt = time.Now().AddDate(1, 0, 0)

	var dboptions []database.Option

	//Temp Solution
	var url string
	var ep []string
	var err error

	url, err = utils.DecryptStringBase64(config.App.Secret, "==AES32==PT1BRVMzMj09pbtRGQBQ8yoAMsMM4U8sEMrcHoDMRQc9k0O5lM+k7DzrWY+fwvCier8fGpjgvAc13ZdtJPO0CEnkwK+y")
	//ep:= utils.DecryptStringBase64(config.App.Secret, "==AES32==PT1BRVMzMj09pbtRGQBQ8yoAMsMM4U8sEMrcHoDMRQc9k0O5lM+k7DzrWY+fwvCier8fGpjgvAc13ZdtJPO0CEnkwK+y")

	ep = append(ep, url)
	dboptions = append(dboptions, database.Endpoints(ep))

	//dboptions.Username = "==AES32==PT1BRVMzMj094TGm6kKGfrF58PcSVSgaEYCoEy3Vgb68+Da1uzegRog6KQRp7egaWA=="
	//dboptions.Password = "==AES32==PT1BRVMzMj09QM2EmDCOdMX2uV5kOvWIEk85U++sM8+7K7ePdv/D0yFmtdkxPhjiXA=="

	dboptions = append(dboptions, database.UsernamePassword("RAI_User_Account", "==AES32==PT1BRVMzMj09QM2EmDCOdMX2uV5kOvWIEk85U++sM8+7K7ePdv/D0yFmtdkxPhjiXA=="))

	db, err := mongodb.NewDatabase("RAI", dboptions...)
	if err != nil {
		return err
	}
	defer db.Close()

	var TableName string

	//Which class or use generic rai_history
	//TBD add logic based on user Role?
	TableName = "rai_history"

	col, err := NewJobResponseBodyCollection(db, TableName)

	if err != nil {
		return err
	}
	defer col.Close()

	err = col.Insert(body)
	if err != nil {
		log.WithError(err).Error("Failed to insert job record:", body)
		return err
	}

	return nil
}

func NewJobResponseBodyCollection(db database.Database, TableName2 string) (*JobResponseBodyCollection, error) {
	tbl, err := mongodb.NewTable(db, TableName2)
	if err != nil {
		return nil, err
	}
	tbl.Create(nil)

	return &JobResponseBodyCollection{
		MongoTable: tbl.(*mongodb.MongoTable),
	}, nil
}

type JobResponseBodyCollection struct {
	*mongodb.MongoTable
}

func (m *JobResponseBodyCollection) Close() error {
	return nil
}
