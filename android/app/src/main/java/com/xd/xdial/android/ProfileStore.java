package com.xd.xdial.android;

import android.content.Context;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.util.AtomicFile;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.security.KeyStore;
import javax.crypto.*;
import javax.crypto.spec.GCMParameterSpec;

// Keep the entire profile encrypted: protocol URLs may themselves carry secrets.
final class ProfileStore {
 private final AtomicFile file;
 ProfileStore(Context c) { file = new AtomicFile(new File(c.getNoBackupFilesDir(), "profile.enc")); }
 private javax.crypto.SecretKey key() throws Exception {
  KeyStore store = KeyStore.getInstance("AndroidKeyStore"); store.load(null);
  if (!store.containsAlias("xdial-profile")) {
   KeyGenerator generator = KeyGenerator.getInstance("AES", "AndroidKeyStore");
   generator.init(new KeyGenParameterSpec.Builder("xdial-profile", KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
    .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build());
   generator.generateKey();
  }
  return (javax.crypto.SecretKey)store.getKey("xdial-profile", null);
 }
 synchronized void save(String text) throws Exception {
  Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding"); cipher.init(Cipher.ENCRYPT_MODE, key());
  byte[] encrypted = cipher.doFinal(text.getBytes(StandardCharsets.UTF_8));
  FileOutputStream out = file.startWrite();
  try { out.write(cipher.getIV()); out.write(encrypted); file.finishWrite(out); }
  catch (Exception e) { file.failWrite(out); throw e; }
 }
 synchronized String load() throws Exception {
  byte[] bytes = file.readFully();
  if (bytes.length < 28) throw new IOException("Invalid encrypted profile");
  Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
  cipher.init(Cipher.DECRYPT_MODE, key(), new GCMParameterSpec(128, bytes, 0, 12));
  return new String(cipher.doFinal(bytes, 12, bytes.length - 12), StandardCharsets.UTF_8);
 }
}
