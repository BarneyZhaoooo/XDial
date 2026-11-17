package com.xd.xdial.android;

import android.content.Context;
import android.util.AtomicFile;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import javax.crypto.Cipher;
import javax.crypto.spec.GCMParameterSpec;
import libbox.SecureStateStore;

/** Tailscale receives an encrypted StateStore, never a plaintext state file. */
final class IdentityStore implements SecureStateStore {
 private final File directory;
 private final Context context;
 IdentityStore(Context context){this.context=context.getApplicationContext();directory=new File(context.getNoBackupFilesDir(),"identities");}
 private byte[] associated(String namespace,String key)throws Exception{
  String resolved=new ProfileStore(context).identityNamespace(context.getNoBackupFilesDir().getAbsolutePath(),namespace);
  return ("xdial-tailscale-v1\u0000"+resolved+"\u0000"+key).getBytes(StandardCharsets.UTF_8);
 }
 private AtomicFile file(byte[] aad)throws Exception{
  if(!directory.isDirectory()&&!directory.mkdirs())throw new IOException("Identity storage unavailable");
  if(!directory.canRead()||!directory.canWrite())throw new IOException("Identity storage inaccessible");
  byte[] digest=MessageDigest.getInstance("SHA-256").digest(aad);StringBuilder name=new StringBuilder();
  for(byte part:digest)name.append(String.format(java.util.Locale.ROOT,"%02x",part&255));
  return new AtomicFile(new File(directory,name+".enc"));
 }
 // Debug acceptance uses independent non-secret records and always deletes them.
 static String verify(Context context)throws Exception{
  IdentityStore store=new IdentityStore(context);String ns="acceptance-"+java.util.UUID.randomUUID();
  byte[] fixture="non-secret-identity-fixture".getBytes(StandardCharsets.UTF_8);
  try{
   store.writeState(ns,"one",fixture);
   if(!java.util.Arrays.equals(new IdentityStore(context).readState(ns,"one"),fixture))throw new IOException("Persistence failed");
   if(store.readState(ns,"missing")!=null)throw new IOException("Missing record mismatch");
   byte[] ciphertext=store.file(store.associated(ns,"one")).readFully();
   AtomicFile copied=store.file(store.associated(ns,"two"));FileOutputStream out=copied.startWrite();
   try{out.write(ciphertext);copied.finishWrite(out);}catch(Exception error){copied.failWrite(out);throw error;}
   boolean denied=false;try{store.readState(ns,"two");}catch(javax.crypto.AEADBadTagException expected){denied=true;}
   if(!denied)throw new IOException("Record substitution accepted");
   store.writeState(ns,"one",null);
   if(store.readState(ns,"one")!=null)throw new IOException("Deletion failed");
   return "Keystore 持久化、记录隔离、篡改拒绝、删除：通过";
  }finally{store.writeState(ns,"one",null);store.writeState(ns,"two",null);}
 }
 public synchronized byte[] readState(String namespace,String key)throws Exception{
  byte[] aad=associated(namespace,key),encoded;
  AtomicFile source=file(aad);
  try{encoded=source.readFully();}catch(FileNotFoundException missing){if(source.getBaseFile().exists())throw new IOException("Identity storage inaccessible");return null;}
  if(encoded.length<28 || encoded.length>8*1024*1024)throw new IOException("Invalid encrypted identity");
  Cipher cipher=Cipher.getInstance("AES/GCM/NoPadding");
  cipher.init(Cipher.DECRYPT_MODE,ProfileStore.key(),new GCMParameterSpec(128,encoded,0,12));cipher.updateAAD(aad);
  return cipher.doFinal(encoded,12,encoded.length-12);
 }
 public synchronized void writeState(String namespace,String key,byte[] value)throws Exception{
  byte[] aad=associated(namespace,key);AtomicFile target=file(aad);
  if(value==null){target.delete();return;}
  if(value.length>8*1024*1024-28)throw new IOException("Identity exceeds storage limit");
  Cipher cipher=Cipher.getInstance("AES/GCM/NoPadding");cipher.init(Cipher.ENCRYPT_MODE,ProfileStore.key());cipher.updateAAD(aad);
  byte[] encoded=cipher.doFinal(value);FileOutputStream out=target.startWrite();
  try{out.write(cipher.getIV());out.write(encoded);target.finishWrite(out);}catch(Exception error){target.failWrite(out);throw error;}
 }
}
