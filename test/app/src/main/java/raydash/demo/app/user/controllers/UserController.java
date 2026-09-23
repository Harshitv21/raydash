package raydash.demo.app.user.controllers;

import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import raydash.demo.app.user.services.impl.UserImpl;
import raydash.demo.app.user.dtos.response.UserDetailResponseDto;

@RestController
@RequestMapping("demo/")
public class UserController {
    private final UserImpl userImplObject;

    public UserController(UserImpl userImplObject) {
        this.userImplObject = userImplObject;
    }

    @GetMapping("/user/{id}")
    public ResponseEntity<UserDetailResponseDto> getUserDetails(@PathVariable Long id) {
        UserDetailResponseDto response = userImplObject.getUserDetails(id);
        
        return new ResponseEntity<>(response, HttpStatus.OK);
    }
}
