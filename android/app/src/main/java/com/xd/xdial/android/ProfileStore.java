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
import libbox.Libbox;

// Keep the entire profile encrypted: protocol URLs may themselves carry secrets.
final class ProfileStore {
 private final AtomicFile file,legacy;
 private static final byte[] LIBRARY_AAD="xdial-profile-library-v3".getBytes(StandardCharsets.UTF_8);
 ProfileStore(Context c) {
  this(c.getNoBackupFilesDir());
 }
 private ProfileStore(File directory) {
  file=new AtomicFile(new File(directory,"profiles.enc"));
  legacy=new AtomicFile(new File(directory,"profile.enc"));
 }
 static synchronized javax.crypto.SecretKey key() throws Exception {return key(true);}
 private static synchronized javax.crypto.SecretKey key(boolean create) throws Exception {
  KeyStore store = KeyStore.getInstance("AndroidKeyStore"); store.load(null);
  if (!store.containsAlias("xdial-profile")) {
   if(!create)throw new IOException("Encrypted configuration key unavailable");
   KeyGenerator generator = KeyGenerator.getInstance("AES", "AndroidKeyStore");
   generator.init(new KeyGenParameterSpec.Builder("xdial-profile", KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
    .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build());
   generator.generateKey();
  }
  return (javax.crypto.SecretKey)store.getKey("xdial-profile", null);
 }
 void save(String text) throws Exception {
  synchronized(ProfileStore.class){writeLibrary(Libbox.applyProfileLibraryRuntime(readLibrary(),text));}
 }
 void saveIdleScenario(String text)throws Exception{
  synchronized(ProfileStore.class){if(TunnelService.active)throw new IOException("Connection is active");writeLibrary(Libbox.applyProfileLibraryRuntime(readLibrary(),text));}
 }
 void saveSelection(String text)throws Exception{
  synchronized(ProfileStore.class){writeLibrary(Libbox.applyProfileLibrarySelection(readLibrary(),text));}
 }
 String load() throws Exception {synchronized(ProfileStore.class){return Libbox.profileLibraryRuntime(readLibrary());}}
 String librarySnapshot()throws Exception{synchronized(ProfileStore.class){return readLibrary();}}
 String remoteSource(String id)throws Exception{synchronized(ProfileStore.class){return Libbox.profileLibraryRemoteSource(readLibrary(),id);}}
 String summary()throws Exception{synchronized(ProfileStore.class){return Libbox.profileLibrarySummary(readLibrary());}}
 String prepareTailscale()throws Exception{
  synchronized(ProfileStore.class){org.json.JSONObject result=new org.json.JSONObject(Libbox.prepareProfileLibraryTailscaleSetup(readLibrary()));writeLibrary(result.getString("library"));return result.getString("line_id");}
 }
 void selectSource(String id)throws Exception{synchronized(ProfileStore.class){writeLibrary(Libbox.selectProfileLibrarySource(readLibrary(),id));}}
 void removeSource(String id)throws Exception{synchronized(ProfileStore.class){writeLibrary(Libbox.removeProfileLibrarySource(readLibrary(),id));}}
 String identityNamespace(String base,String namespace)throws Exception{
  synchronized(ProfileStore.class){return Libbox.resolveProfileLibraryIdentityNamespace(readLibrary(),base,namespace);}
 }
 String preview(String content,String name,String updatingID)throws Exception{
  synchronized(ProfileStore.class){return Libbox.previewNativeProfileImport(readLibrary(),content,name,updatingID);}
 }
 void commitPreview(String preview)throws Exception{
  synchronized(ProfileStore.class){if(TunnelService.active)throw new IOException("Connection is active");writeLibrary(Libbox.commitProfileLibraryPreview(readLibrary(),preview));}
 }
 private String readLibrary()throws Exception{
  byte[] bytes;
  try{bytes=file.readFully();}catch(FileNotFoundException missing){
   if(file.getBaseFile().exists())throw new IOException("Configuration storage inaccessible");
   String original="";try{original=decrypt(readLegacy(),false);}catch(FileNotFoundException noLegacy){if(legacy.getBaseFile().exists()||new File(legacy.getBaseFile()+".bak").exists())throw new IOException("Legacy configuration inaccessible");}
   String library=Libbox.newProfileLibrary(original);writeLibrary(library);return library;
  }
  String library=decrypt(bytes,true);Libbox.profileLibrarySummary(library);return library;
 }
 private byte[] readLegacy()throws Exception{
  // AtomicFile.openRead may restore/delete legacy backups. Migration reads the
  // last committed backup when present without modifying any old file.
  File backup=new File(legacy.getBaseFile()+".bak"),source=backup.exists()?backup:legacy.getBaseFile();
  try(InputStream input=new FileInputStream(source);ByteArrayOutputStream output=new ByteArrayOutputStream()){
   byte[] buffer=new byte[8192];int count;while((count=input.read(buffer))!=-1){if(output.size()+count>8*1024*1024+28)throw new IOException("Legacy configuration exceeds limit");output.write(buffer,0,count);}return output.toByteArray();
  }
 }
 private void writeLibrary(String text) throws Exception {
  Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding"); cipher.init(Cipher.ENCRYPT_MODE, key());
  cipher.updateAAD(LIBRARY_AAD);
  byte[] encrypted = cipher.doFinal(text.getBytes(StandardCharsets.UTF_8));
  FileOutputStream out = file.startWrite();
  try { out.write(cipher.getIV()); out.write(encrypted); file.finishWrite(out); }
  catch (Exception e) { file.failWrite(out); throw e; }
 }
 private String decrypt(byte[] bytes,boolean library) throws Exception {
  if (bytes.length < 28) throw new IOException("Invalid encrypted profile");
  Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
  cipher.init(Cipher.DECRYPT_MODE, key(false), new GCMParameterSpec(128, bytes, 0, 12));
  if(library)cipher.updateAAD(LIBRARY_AAD);
  return new String(cipher.doFinal(bytes, 12, bytes.length - 12), StandardCharsets.UTF_8);
 }
 // Isolated, non-secret acceptance records exercise the real Android Keystore
 // and AtomicFile path. Never corrupt or overwrite the user's configuration.
 static String verify(Context context)throws Exception{
  File directory=new File(context.getNoBackupFilesDir(),"library-acceptance-"+java.util.UUID.randomUUID());
  if(!directory.mkdir())throw new IOException("Acceptance directory unavailable");
  ProfileStore store=new ProfileStore(directory);
  try{
   String fixture="{\"lines\":[{\"id\":\"direct\",\"type\":\"direct\",\"enabled\":true}],\"scenarios\":[{\"id\":\"old\",\"name\":\"Fixture\",\"default_line_id\":\"direct\"}],\"active_scenario_id\":\"old\",\"custom_fixture\":7}";
   Cipher cipher=Cipher.getInstance("AES/GCM/NoPadding");cipher.init(Cipher.ENCRYPT_MODE,key());byte[] encrypted=cipher.doFinal(fixture.getBytes(StandardCharsets.UTF_8));
   FileOutputStream out=store.legacy.startWrite();try{out.write(cipher.getIV());out.write(encrypted);store.legacy.finishWrite(out);}catch(Exception e){store.legacy.failWrite(out);throw e;}
   byte[] before=store.legacy.readFully();
   File backup=new File(store.legacy.getBaseFile()+".bak");try(FileOutputStream copy=new FileOutputStream(backup)){copy.write(before);}
   org.json.JSONObject runtime=new org.json.JSONObject(store.load());
   if(!runtime.getString("active_scenario_id").equals("old")||runtime.getInt("custom_fixture")!=7)throw new IOException("Migration lost selection");
   runtime.put("mobile_automatic",false);store.save(runtime.toString());
   if(new org.json.JSONObject(new ProfileStore(directory).load()).getBoolean("mobile_automatic"))throw new IOException("Library write not persisted");
   if(!java.util.Arrays.equals(before,java.nio.file.Files.readAllBytes(store.legacy.getBaseFile().toPath()))||!java.util.Arrays.equals(before,java.nio.file.Files.readAllBytes(backup.toPath())))throw new IOException("Legacy file or backup changed");
   byte[] corrupt=store.file.readFully();corrupt[corrupt.length-1]^=1;
   out=store.file.startWrite();try{out.write(corrupt);store.file.finishWrite(out);}catch(Exception e){store.file.failWrite(out);throw e;}
   boolean rejected=false;try{new ProfileStore(directory).load();}catch(javax.crypto.AEADBadTagException expected){rejected=true;}
   if(!rejected)throw new IOException("Corrupt library fell back to legacy");
   return "配置库迁移、旧文件保留、重读、损坏拒绝：通过";
  }finally{store.file.delete();store.legacy.delete();directory.delete();}
 }
}
