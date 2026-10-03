package raydash.demo.app.user.services;

import raydash.demo.app.user.dtos.response.UserDetailResponseDto;

public interface UserService {
    UserDetailResponseDto getUserDetails(Long id);
}
