# CMDS

## PING

### Command

```cmd
TING
```

### Expected Output

```cmd
TONG
```

## SET

### Command

```cmd
SET username harshit
```

### Expected Output

```cmd
+OK
```

## GET

### Command

```cmd
GET username
```

### Expected Output

```cmd
harshit
```

## DEL

### Sequence - I

#### Command

```cmd
DEL username
```

#### Expected Output

```cmd
+OK
```

### Sequence - II

#### Command

```cmd
GET username
```

#### Expected Output

```cmd
$-1
```

## EXPIRE & TTL

### Sequence - I

#### Command

```cmd
SET session xyz
```

#### Expected Output

```cmd
+OK
```

### Sequence - II

#### Command

```cmd
TTL session
```

#### Expected Output

```cmd
$-1
```

### Sequence - III

#### Command

```cmd
EXPIRE session 15
```

#### Expected Output

```cmd
+OK
```

### Sequence - IV

#### Command

```cmd
TTL session
```

#### Expected Output

> This number could vary!

```cmd
:13
```

> Wait 15 seconds for the key to die before executing this next command

### Sequence - V

#### Command

```cmd
GET session
```

#### Expected Output

```cmd
$-1
```

### Sequence - VI

#### Command

```cmd
TTL session
```

#### Expected Output

> key does not exist anymore

```cmd
$-2
```

## Crash & Error Resilience Test

### Missing Arugument Command

#### Command

```cmd
SET novalueprovided
```

#### Expected Output

```cmd
-ERR wrong number of arguments for 'set' command
```

### Unknown Command

#### Command

```cmd
UNKNOWN_COMMAND arg1
```

#### Expected Output

```cmd
-ERR unknown command 'unknown_command'
````

---
