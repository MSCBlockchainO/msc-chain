# MSC Ambassador Firebase Setup

The portal is configured for this Firebase Realtime Database:

```text
https://msc-blockchain-infulaner-data-default-rtdb.asia-southeast1.firebasedatabase.app
```

`firebase-config.js` already contains the provided Web App configuration with:

```js
enabled: true
databaseMode: "realtime"
```

## Firebase Console

1. Open the `msc-blockchain-infulaner-data` Firebase project.
2. Enable Authentication with Email/Password.
3. Open Realtime Database.
4. Publish `database.rules.json`.
5. Create one Firebase Authentication user with this email: `admin@msc.com`.

Open `admin.html` and sign in with the `admin@msc.com` email/password account. The portal does not use Google or phone login for admin access, even if those providers are enabled in the Firebase console.

Serve the portal through HTTP/HTTPS. Do not open it only as a `file://` page when testing Firebase Authentication.

Add the production portal domain in Firebase Console under Authentication, Settings, Authorized domains.

## Deploy Rules

Run from the repository root:

```powershell
Push-Location ui\ambassador
firebase deploy --config firebase.json --only database --project msc-blockchain-infulaner-data
Pop-Location
```

## Realtime Database Paths

- `msc_ambassador_applications`
- `msc_ambassador_ambassadors`
- `msc_ambassador_influencers`
- `msc_ambassador_users`
- `msc_ambassador_referrals`
- `msc_ambassador_referral_claims` (private names/emails)
- `msc_ambassador_influencer_referrals`
- `msc_ambassador_referral_codes`
- `msc_ambassador_rewards`
- `msc_ambassador_influencer_rewards`
- `msc_ambassador_campaigns`
- `msc_ambassador_campaign_members`
- `msc_ambassador_settings`
- `msc_ambassador_announcements`
- `msc_ambassador_contacts`
- `msc_ambassador_admins`

## Roles

- Super Admin: exactly one admin email, `admin@msc.com`.
- Influencers: unlimited approved ambassador profiles with referral code, reputation, referral count, status, verified badge, Founder NFT eligibility, and reward history.
- Normal Users: unlimited referral joins with name, email/email hash, referred by, join date, and locked referral reward marker.

Recommended flow:

```text
Normal user -> Apply for Ambassador -> Admin review -> Approve -> Influencer profile active -> Referral code generated -> Reputation and rewards tracked
```

The admin can approve or reject applications, assign locked rewards, publish announcements, manage leaderboard visibility, suspend or ban influencers, and update settings. Suspended or banned influencers are hidden from public ranking/referral selection.

Approved influencers can use `influencer.html` with their email/password account and referral code.

After approving an influencer:

1. Open Firebase Console.
2. Go to Authentication, then Users.
3. Click Add user.
4. Use the approved influencer email from the application.
5. Set a temporary password.
6. Send the influencer `influencer.html`, their referral code, and the temporary password.
7. In the admin dashboard, click Auth Ready for that influencer after the Auth user exists.

Do not create influencer Auth users directly from public browser code. Automatic Auth user creation should use a trusted backend, such as Firebase Cloud Functions with the Firebase Admin SDK.

## Optional Firestore Mode

The portal still supports Firestore. Change:

```js
databaseMode: "firestore"
```

Then enable Cloud Firestore and deploy `firestore.rules`.

## Important

The Firebase Web App configuration is public client configuration. Access control comes from Firebase Authentication and database rules.

Only `admin@msc.com` can read admin-only data or approve/reject applications. Normal users can submit applications, referrals, and contact messages but cannot open the admin dashboard.

The local demo admin key is used only when Firebase is disabled or has placeholder configuration.
