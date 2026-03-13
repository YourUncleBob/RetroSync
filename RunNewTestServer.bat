call buildall.bat
cd newTest
call CleanTestFiles.bat
cd ..
RetroSync -config newTest/Server/testServerConfig.toml -paused
