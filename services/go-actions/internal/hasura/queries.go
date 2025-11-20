package hasura

// GraphQL queries and mutations

const (
	// CheckUserExistsQuery checks if a user exists
	CheckUserExistsQuery = `
		query CheckUser($id: uuid!) {
			users_by_pk(id: $id) {
				id
			}
		}
	`

	// GetUserQuery retrieves full user data
	GetUserQuery = `
		query GetUser($id: uuid!) {
			users_by_pk(id: $id) {
				id
				display_name
				avatar_url
				account {
					email
				}
			}
		}
	`

	// CreateUserMutation creates a new user (without account)
	CreateUserMutation = `
		mutation CreateUser($id: uuid!, $display_name: String!) {
			insert_users_one(
				object: {
					id: $id
					display_name: $display_name
				}
				on_conflict: {
					constraint: users_pkey
					update_columns: [display_name]
				}
			) {
				id
				display_name
				avatar_url
			}
		}
	`

	// CreateAccountMutation creates or updates an account for a user
	CreateAccountMutation = `
		mutation CreateAccount($user_id: uuid!, $email: citext!) {
			insert_auth_accounts_one(
				object: {
					user_id: $user_id
					email: $email
					active: true
				}
				on_conflict: {
					constraint: accounts_user_id_key
					update_columns: [email]
				}
			) {
				id
				user_id
				email
			}
		}
	`

	// CreateDefaultTeamMutation creates a default team for a user
	CreateDefaultTeamMutation = `
		mutation CreateDefaultTeam($user_id: uuid!) {
			insert_teams_one(
				object: {
					name: "Default team"
					members: {
						data: {
							user_id: $user_id
							member_roles: {
								data: { team_role: owner }
							}
						}
					}
				}
			) {
				id
			}
		}
	`
)
