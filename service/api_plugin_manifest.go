package service

import (
	"context"
	"net/http"
	"time"

	"github.com/evergreen-ci/evergreen/model"
	"github.com/evergreen-ci/evergreen/model/manifest"
	"github.com/evergreen-ci/evergreen/thirdparty"
	"github.com/evergreen-ci/gimlet"
	"github.com/google/go-github/github"
	"github.com/pkg/errors"
)

// manifestLoadHandler attempts to get the manifest, if it exists it updates the expansions and returns
// If it does not exist it performs GitHub API calls for each of the project's modules and gets
// the head revision of the branch and inserts it into the manifest collection.
// If there is a duplicate key error, then do a find on the manifest again.
func (as *APIServer) manifestLoadHandler(w http.ResponseWriter, r *http.Request) {
	task := MustHaveTask(r)

	projectRef, err := model.FindOneProjectRef(task.Project)
	if err != nil {
		as.LoggedError(w, r, http.StatusBadRequest,
			errors.Wrapf(err, "projectRef not found for project %s", task.Project))
		return
	}

	project, err := model.FindProject("", projectRef)
	if err != nil {
		as.LoggedError(w, r, http.StatusBadRequest,
			errors.Wrapf(err, "project not found for ProjectRef %s", projectRef.Identifier))
		return
	}
	if project == nil {
		as.LoggedError(w, r, http.StatusBadRequest,
			errors.Errorf("empty project not found for ProjectRef %s", projectRef.Identifier))
		return
	}

	// try to get the manifest
	currentManifest, err := manifest.FindOne(manifest.ById(task.Version))
	if err != nil {
		as.LoggedError(w, r, http.StatusBadRequest,
			errors.Wrapf(err, "error retrieving manifest with version id %s", task.Version))
		return
	}
	if currentManifest != nil {
		gimlet.WriteJSON(w, currentManifest)
		return
	}

	if task.Version == "" {
		as.LoggedError(w, r, http.StatusBadRequest,
			errors.Errorf("found empty version when retrieving manifest for %s", projectRef.Identifier))
		return
	}

	// attempt to insert a manifest after making GitHub API calls
	newManifest := &manifest.Manifest{
		Id:          task.Version,
		Revision:    task.Revision,
		ProjectName: task.Project,
		Branch:      projectRef.Branch,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// populate modules
	var gitBranch *github.Branch
	modules := make(map[string]*manifest.Module)
	for _, module := range project.Modules {
		var token string
		owner, repo := module.GetRepoOwnerAndName()
		token, err = as.Settings.GetGithubOauthToken()
		if err != nil {
			as.LoggedError(w, r, http.StatusInternalServerError, errors.Wrap(err, "error getting github token"))
			return
		}
		gitBranch, err = thirdparty.GetBranchEvent(ctx, token, owner, repo, module.Branch)
		if err != nil {
			as.LoggedError(w, r, http.StatusInternalServerError,
				errors.Wrapf(err, "problem retrieving getting git branch for module %s", module.Name))
			return
		}

		modules[module.Name] = &manifest.Module{
			Branch:   module.Branch,
			Revision: *gitBranch.Commit.SHA,
			Repo:     repo,
			Owner:    owner,
			URL:      *gitBranch.Commit.URL,
		}
	}
	newManifest.Modules = modules
	duplicate, err := newManifest.TryInsert()
	if err != nil {
		as.LoggedError(w, r, http.StatusInternalServerError,
			errors.Wrapf(err, "problem inserting manifest for project %s", newManifest.ProjectName))
		return
	}
	// if it is a duplicate, load the manifest again`
	if duplicate {
		// try to get the manifest
		m, err := manifest.FindOne(manifest.ById(task.Version))
		if err != nil {
			as.LoggedError(w, r, http.StatusBadRequest,
				errors.Wrapf(err, "problem getting latest manifest for project %s", newManifest.ProjectName))
			return
		}
		if m != nil {
			gimlet.WriteJSON(w, m)
			return
		}
	}

	// no duplicate key error, use the manifest just created.

	gimlet.WriteJSON(w, newManifest)
}
