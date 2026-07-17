// Firebase config for the real MSC Ambassador portal.
// Replace the placeholder values with your Firebase Web App config and set enabled to true.
// Firebase client config is not a private secret, but Firestore rules must protect admin data.
window.MSC_AMBASSADOR_FIREBASE = {
  enabled: true,
  sdkVersion: "12.15.0",
  databaseMode: "realtime",
  collectionPrefix: "msc_ambassador",
  adminEmail: "admin@msc.com",
  firebaseConfig: {
    apiKey: "AIzaSyCvY3PGmlfd2neo7hF_YCLphtZfQqB4huo",
    authDomain: "msc-blockchain-infulaner-data.firebaseapp.com",
    databaseURL: "https://msc-blockchain-infulaner-data-default-rtdb.asia-southeast1.firebasedatabase.app",
    projectId: "msc-blockchain-infulaner-data",
    storageBucket: "msc-blockchain-infulaner-data.firebasestorage.app",
    messagingSenderId: "465678235932",
    appId: "1:465678235932:web:767cefd40ef5a42e85e2d9",
    measurementId: "G-CK9PNFD36Z",
  },
};
